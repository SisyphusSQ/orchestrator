/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	nethttp "net/http"
	"strings"

	"github.com/openark/orchestrator/internal/agent"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/http"
	httpagent "github.com/openark/orchestrator/internal/http/agent"
	httpobservability "github.com/openark/orchestrator/internal/http/observability"
	"github.com/openark/orchestrator/internal/http/transport"
	httpweb "github.com/openark/orchestrator/internal/http/web"
	instmaintenance "github.com/openark/orchestrator/internal/inst/maintenance"
	"github.com/openark/orchestrator/internal/kv"
	"github.com/openark/orchestrator/internal/logic/discovery"
	"github.com/openark/orchestrator/internal/process"
	"github.com/openark/orchestrator/internal/ssl"
	webassets "github.com/openark/orchestrator/web"

	"github.com/openark/orchestrator/internal/golib/log"
)

var sslPEMPassword []byte
var agentSSLPEMPassword []byte

// Http starts serving
func Http(continuousDiscovery bool) (resultErr error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := config.Config.ValidateRaft(); err != nil {
		return fmt.Errorf("validate raft configuration: %w", err)
	}
	if err := kv.InitKVStores(); err != nil {
		return fmt.Errorf("initialize KV stores: %w", err)
	}
	if err := startRaftRuntime(); err != nil {
		return err
	}
	defer func() {
		cancel()
		resultErr = errors.Join(resultErr, CloseRaftRuntime())
	}()
	discovery.AcceptSignals()
	promptForSSLPasswords()
	closeMonitor := startHealthMonitor()
	defer closeMonitor()
	process.ContinuousRegistration(process.OrchestratorExecutionHttpMode, "")

	runtimeErrors := make(chan error, 4)
	go reportRuntimeError(runtimeErrors, "raft runtime", func() error { return monitorRaft(ctx) })
	go reportRuntimeError(runtimeErrors, "standard HTTP server", func() error {
		return standardHttp(ctx, continuousDiscovery, runtimeErrors)
	})
	if config.Config.Agents.ServeHTTP {
		go reportRuntimeError(runtimeErrors, "agent HTTP server", agentsHttp)
	}
	return <-runtimeErrors
}

func reportRuntimeError(runtimeErrors chan<- error, component string, run func() error) {
	err := run()
	if err == nil {
		err = fmt.Errorf("stopped without an error")
	}
	runtimeErrors <- fmt.Errorf("%s: %w", component, err)
}

// Iterate over the private keys and get passwords for them
// Don't prompt for a password a second time if the files are the same
func promptForSSLPasswords() {
	if ssl.IsEncryptedPEM(config.Config.Server.TLS.PrivateKeyFile) {
		sslPEMPassword = ssl.GetPEMPassword(config.Config.Server.TLS.PrivateKeyFile)
	}
	if ssl.IsEncryptedPEM(config.Config.Agents.TLS.PrivateKeyFile) {
		if config.Config.Agents.TLS.PrivateKeyFile == config.Config.Server.TLS.PrivateKeyFile {
			agentSSLPEMPassword = sslPEMPassword
		} else {
			agentSSLPEMPassword = ssl.GetPEMPassword(config.Config.Agents.TLS.PrivateKeyFile)
		}
	}
}

// standardHttp starts serving HTTP or HTTPS (api/web) requests, to be used by normal clients
func standardHttp(ctx context.Context, continuousDiscovery bool, runtimeErrors chan<- error) error {
	m, err := newStandardHTTPRouter()
	if err != nil {
		return err
	}

	instmaintenance.SetMaintenanceOwner(process.ThisHostname)

	if continuousDiscovery {

		log.Info("Starting Discovery")
		go reportRuntimeError(runtimeErrors, "continuous discovery", func() error { return discovery.ContinuousDiscovery(ctx) })
	}

	log.Info("Registering endpoints")

	// Serve
	if config.Config.Server.Listen.Socket != "" {
		log.Infof("Starting HTTP listener on unix socket %v", config.Config.Server.Listen.Socket)
		unixListener, err := net.Listen("unix", config.Config.Server.Listen.Socket)
		if err != nil {
			return fmt.Errorf("listen on unix socket %s: %w", config.Config.Server.Listen.Socket, err)
		}
		defer unixListener.Close()
		if err := nethttp.Serve(unixListener, m); err != nil {
			return fmt.Errorf("serve HTTP on unix socket %s: %w", config.Config.Server.Listen.Socket, err)
		}
	} else if config.Config.Server.TLS.Enabled {
		log.Info("Starting HTTPS listener")
		tlsConfig, err := ssl.NewTLSConfig(config.Config.Server.TLS.CAFile, config.Config.Server.TLS.MutualTLS)
		if err != nil {
			return fmt.Errorf("create HTTP TLS configuration: %w", err)
		}
		tlsConfig.InsecureSkipVerify = config.Config.Server.TLS.SkipVerify
		if err = ssl.AppendKeyPairWithPassword(tlsConfig, config.Config.Server.TLS.CertFile, config.Config.Server.TLS.PrivateKeyFile, sslPEMPassword); err != nil {
			return fmt.Errorf("load HTTP TLS key pair: %w", err)
		}
		if err = ssl.ListenAndServeTLS(config.Config.Server.Listen.Address, m, tlsConfig); err != nil {
			return fmt.Errorf("serve HTTPS on %s: %w", config.Config.Server.Listen.Address, err)
		}
	} else {
		log.Infof("Starting HTTP listener on %+v", config.Config.Server.Listen.Address)
		if err := nethttp.ListenAndServe(config.Config.Server.Listen.Address, m); err != nil {
			return fmt.Errorf("serve HTTP on %s: %w", config.Config.Server.Listen.Address, err)
		}
	}
	log.Info("Web server started")
	return nil
}

// agentsHttp startes serving agents HTTP or HTTPS API requests
func agentsHttp() error {
	m, err := newAgentsHTTPRouter()
	if err != nil {
		return err
	}

	log.Info("Starting agents listener")

	agent.InitHttpClient()
	go discovery.ContinuousAgentsPoll()

	// Serve
	if config.Config.Agents.TLS.Enabled {
		log.Info("Starting agent HTTPS listener")
		tlsConfig, err := ssl.NewTLSConfig(config.Config.Agents.TLS.CAFile, config.Config.Agents.TLS.MutualTLS)
		if err != nil {
			return fmt.Errorf("create agent HTTP TLS configuration: %w", err)
		}
		tlsConfig.InsecureSkipVerify = config.Config.Agents.TLS.SkipVerify
		if err = ssl.AppendKeyPairWithPassword(tlsConfig, config.Config.Agents.TLS.CertFile, config.Config.Agents.TLS.PrivateKeyFile, agentSSLPEMPassword); err != nil {
			return fmt.Errorf("load agent HTTP TLS key pair: %w", err)
		}
		if err = ssl.ListenAndServeTLS(config.Config.Agents.ServerPort, m, tlsConfig); err != nil {
			return fmt.Errorf("serve agent HTTPS on %s: %w", config.Config.Agents.ServerPort, err)
		}
	} else {
		log.Info("Starting agent HTTP listener")
		if err := nethttp.ListenAndServe(config.Config.Agents.ServerPort, m); err != nil {
			return fmt.Errorf("serve agent HTTP on %s: %w", config.Config.Agents.ServerPort, err)
		}
	}
	log.Info("Agent server started")
	return nil
}

func newStandardHTTPRouter() (*transport.Router, error) {
	if strings.EqualFold(config.Config.Authentication.Method, "basic") && config.Config.Authentication.Basic.User == "" {
		// Still allowed; may be disallowed in future versions.
		log.Warning("authentication.method is configured as 'basic' but authentication.basic.user is undefined. Running without authentication.")
	}

	options := transport.RouterOptions{
		Authentication: transport.AuthenticationOptions{
			Method:   config.Config.Authentication.Method,
			Username: config.Config.Authentication.Basic.User,
			Password: config.Config.Authentication.Basic.Password,
		},
		EnableGzip: true,
	}
	if config.Config.Server.TLS.MutualTLS {
		options.VerifyRequest = ssl.VerifyOUs(config.Config.Server.TLS.ValidOUs)
	}

	router, err := transport.NewRouter(options)
	if err != nil {
		return nil, err
	}
	assets, err := fs.Sub(webassets.Files(), "assets")
	if err != nil {
		return nil, fmt.Errorf("open embedded web assets: %w", err)
	}
	router.StaticFS(config.Config.Server.URLPrefix+"/web/assets", nethttp.FS(assets))

	api := http.Routes{URLPrefix: config.Config.Server.URLPrefix}
	web := httpweb.New(config.Config.Server.URLPrefix, nil)
	httpobservability.Register(router, config.Config.Server.URLPrefix)
	api.RegisterRequests(router)
	web.RegisterRequests(router)
	return router, nil
}

func newAgentsHTTPRouter() (*transport.Router, error) {
	options := transport.RouterOptions{EnableGzip: true}
	if config.Config.Agents.TLS.MutualTLS {
		options.VerifyRequest = ssl.VerifyOUs(config.Config.Agents.TLS.ValidOUs)
	}
	router, err := transport.NewRouter(options)
	if err != nil {
		return nil, err
	}
	agentAPI := httpagent.RegistrationAPI{URLPrefix: config.Config.Server.URLPrefix}
	agentAPI.RegisterRequests(router)
	return router, nil
}
