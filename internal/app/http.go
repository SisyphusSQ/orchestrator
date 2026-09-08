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
	"github.com/openark/orchestrator/internal/inst"
	"github.com/openark/orchestrator/internal/kv"
	"github.com/openark/orchestrator/internal/logic"
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
	logic.AcceptSignals()
	promptForSSLPasswords()
	closeMonitor := startHealthMonitor()
	defer closeMonitor()
	process.ContinuousRegistration(process.OrchestratorExecutionHttpMode, "")

	runtimeErrors := make(chan error, 4)
	go reportRuntimeError(runtimeErrors, "raft runtime", func() error { return monitorRaft(ctx) })
	go reportRuntimeError(runtimeErrors, "standard HTTP server", func() error {
		return standardHttp(ctx, continuousDiscovery, runtimeErrors)
	})
	if config.Config.ServeAgentsHttp {
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
	if ssl.IsEncryptedPEM(config.Config.SSLPrivateKeyFile) {
		sslPEMPassword = ssl.GetPEMPassword(config.Config.SSLPrivateKeyFile)
	}
	if ssl.IsEncryptedPEM(config.Config.AgentSSLPrivateKeyFile) {
		if config.Config.AgentSSLPrivateKeyFile == config.Config.SSLPrivateKeyFile {
			agentSSLPEMPassword = sslPEMPassword
		} else {
			agentSSLPEMPassword = ssl.GetPEMPassword(config.Config.AgentSSLPrivateKeyFile)
		}
	}
}

// standardHttp starts serving HTTP or HTTPS (api/web) requests, to be used by normal clients
func standardHttp(ctx context.Context, continuousDiscovery bool, runtimeErrors chan<- error) error {
	m, err := newStandardHTTPRouter()
	if err != nil {
		return err
	}

	inst.SetMaintenanceOwner(process.ThisHostname)

	if continuousDiscovery {

		log.Info("Starting Discovery")
		go reportRuntimeError(runtimeErrors, "continuous discovery", func() error { return logic.ContinuousDiscovery(ctx) })
	}

	log.Info("Registering endpoints")

	// Serve
	if config.Config.ListenSocket != "" {
		log.Infof("Starting HTTP listener on unix socket %v", config.Config.ListenSocket)
		unixListener, err := net.Listen("unix", config.Config.ListenSocket)
		if err != nil {
			return fmt.Errorf("listen on unix socket %s: %w", config.Config.ListenSocket, err)
		}
		defer unixListener.Close()
		if err := nethttp.Serve(unixListener, m); err != nil {
			return fmt.Errorf("serve HTTP on unix socket %s: %w", config.Config.ListenSocket, err)
		}
	} else if config.Config.UseSSL {
		log.Info("Starting HTTPS listener")
		tlsConfig, err := ssl.NewTLSConfig(config.Config.SSLCAFile, config.Config.UseMutualTLS)
		if err != nil {
			return fmt.Errorf("create HTTP TLS configuration: %w", err)
		}
		tlsConfig.InsecureSkipVerify = config.Config.SSLSkipVerify
		if err = ssl.AppendKeyPairWithPassword(tlsConfig, config.Config.SSLCertFile, config.Config.SSLPrivateKeyFile, sslPEMPassword); err != nil {
			return fmt.Errorf("load HTTP TLS key pair: %w", err)
		}
		if err = ssl.ListenAndServeTLS(config.Config.ListenAddress, m, tlsConfig); err != nil {
			return fmt.Errorf("serve HTTPS on %s: %w", config.Config.ListenAddress, err)
		}
	} else {
		log.Infof("Starting HTTP listener on %+v", config.Config.ListenAddress)
		if err := nethttp.ListenAndServe(config.Config.ListenAddress, m); err != nil {
			return fmt.Errorf("serve HTTP on %s: %w", config.Config.ListenAddress, err)
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
	go logic.ContinuousAgentsPoll()

	// Serve
	if config.Config.AgentsUseSSL {
		log.Info("Starting agent HTTPS listener")
		tlsConfig, err := ssl.NewTLSConfig(config.Config.AgentSSLCAFile, config.Config.AgentsUseMutualTLS)
		if err != nil {
			return fmt.Errorf("create agent HTTP TLS configuration: %w", err)
		}
		tlsConfig.InsecureSkipVerify = config.Config.AgentSSLSkipVerify
		if err = ssl.AppendKeyPairWithPassword(tlsConfig, config.Config.AgentSSLCertFile, config.Config.AgentSSLPrivateKeyFile, agentSSLPEMPassword); err != nil {
			return fmt.Errorf("load agent HTTP TLS key pair: %w", err)
		}
		if err = ssl.ListenAndServeTLS(config.Config.AgentsServerPort, m, tlsConfig); err != nil {
			return fmt.Errorf("serve agent HTTPS on %s: %w", config.Config.AgentsServerPort, err)
		}
	} else {
		log.Info("Starting agent HTTP listener")
		if err := nethttp.ListenAndServe(config.Config.AgentsServerPort, m); err != nil {
			return fmt.Errorf("serve agent HTTP on %s: %w", config.Config.AgentsServerPort, err)
		}
	}
	log.Info("Agent server started")
	return nil
}

func newStandardHTTPRouter() (*http.Router, error) {
	if strings.EqualFold(config.Config.AuthenticationMethod, "basic") && config.Config.HTTPAuthUser == "" {
		// Still allowed; may be disallowed in future versions.
		log.Warning("AuthenticationMethod is configured as 'basic' but HTTPAuthUser undefined. Running without authentication.")
	}

	options := http.RouterOptions{
		Authentication: http.AuthenticationOptions{
			Method:   config.Config.AuthenticationMethod,
			Username: config.Config.HTTPAuthUser,
			Password: config.Config.HTTPAuthPassword,
		},
		EnableGzip: true,
	}
	if config.Config.UseMutualTLS {
		options.VerifyRequest = ssl.VerifyOUs(config.Config.SSLValidOUs)
	}

	router, err := http.NewRouter(options)
	if err != nil {
		return nil, err
	}
	assets, err := fs.Sub(webassets.Files(), "assets")
	if err != nil {
		return nil, fmt.Errorf("open embedded web assets: %w", err)
	}
	router.StaticFS(config.Config.URLPrefix+"/web/assets", nethttp.FS(assets))

	http.API.URLPrefix = config.Config.URLPrefix
	http.Web.URLPrefix = config.Config.URLPrefix
	http.RegisterObservability(router, config.Config.URLPrefix)
	http.API.RegisterRequests(router)
	http.Web.RegisterRequests(router)
	return router, nil
}

func newAgentsHTTPRouter() (*http.Router, error) {
	options := http.RouterOptions{EnableGzip: true}
	if config.Config.AgentsUseMutualTLS {
		options.VerifyRequest = ssl.VerifyOUs(config.Config.AgentSSLValidOUs)
	}
	router, err := http.NewRouter(options)
	if err != nil {
		return nil, err
	}
	http.AgentsAPI.URLPrefix = config.Config.URLPrefix
	http.AgentsAPI.RegisterRequests(router)
	return router, nil
}
