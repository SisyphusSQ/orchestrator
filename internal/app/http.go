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
	"crypto/tls"
	"errors"
	"fmt"
	"io/fs"
	"net"
	nethttp "net/http"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	instanceapi "github.com/openark/orchestrator/internal/http/api/instance"

	"github.com/openark/orchestrator/internal/agent"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/http"
	httpagent "github.com/openark/orchestrator/internal/http/agent"
	httpobservability "github.com/openark/orchestrator/internal/http/observability"
	"github.com/openark/orchestrator/internal/http/transport"
	httpweb "github.com/openark/orchestrator/internal/http/web"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
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
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()
	if err := config.PrepareRaft(); err != nil {
		return fmt.Errorf("validate raft configuration: %w", err)
	}
	if err := kv.InitKVStores(); err != nil {
		return fmt.Errorf("initialize KV stores: %w", err)
	}
	if err := startRaftRuntime(); err != nil {
		return err
	}
	var services sync.WaitGroup
	defer func() {
		cancel()
		services.Wait()
		resultErr = errors.Join(resultErr, CloseRaftRuntime())
	}()
	stopReloadSignals := discovery.AcceptReloadSignals(ctx)
	defer stopReloadSignals()
	promptForSSLPasswords()
	closeMonitor := startHealthMonitor()
	defer closeMonitor()
	process.ContinuousRegistration(process.OrchestratorExecutionHttpMode, "")

	runtimeErrors := make(chan error, 4)
	go reportRuntimeError(runtimeErrors, "raft runtime", func() error { return monitorRaft(ctx) })
	services.Go(func() {
		reportRuntimeError(runtimeErrors, "standard HTTP server", func() error {
			return standardHttp(ctx, continuousDiscovery, runtimeErrors)
		})
	})
	if config.Current().Agents.ServeHTTP {
		go reportRuntimeError(runtimeErrors, "agent HTTP server", agentsHttp)
	}
	select {
	case err := <-runtimeErrors:
		if ctx.Err() != nil {
			return nil
		}
		return err
	case <-ctx.Done():
		log.Info("Shutting down orchestrator")
		instaudit.AuditOperation("shutdown", nil, "Triggered via termination signal")
		return nil
	}
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
	if ssl.IsEncryptedPEM(config.Current().Server.TLS.PrivateKeyFile) {
		sslPEMPassword = ssl.GetPEMPassword(config.Current().Server.TLS.PrivateKeyFile)
	}
	if ssl.IsEncryptedPEM(config.Current().Agents.TLS.PrivateKeyFile) {
		if config.Current().Agents.TLS.PrivateKeyFile == config.Current().Server.TLS.PrivateKeyFile {
			agentSSLPEMPassword = sslPEMPassword
		} else {
			agentSSLPEMPassword = ssl.GetPEMPassword(config.Current().Agents.TLS.PrivateKeyFile)
		}
	}
}

// standardHttp starts serving HTTP or HTTPS (api/web) requests, to be used by normal clients
func standardHttp(ctx context.Context, continuousDiscovery bool, runtimeErrors chan<- error) error {
	manual := discovery.NewManual(ctx)
	defer manual.Close()
	m, err := newStandardHTTPRouter(manual)
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
	if config.Current().Server.Listen.Socket != "" {
		log.Infof("Starting HTTP listener on unix socket %v", config.Current().Server.Listen.Socket)
		unixListener, err := net.Listen("unix", config.Current().Server.Listen.Socket)
		if err != nil {
			return fmt.Errorf("listen on unix socket %s: %w", config.Current().Server.Listen.Socket, err)
		}
		defer unixListener.Close()
		if err := serveHTTPContext(ctx, unixListener, m); err != nil {
			return fmt.Errorf("serve HTTP on unix socket %s: %w", config.Current().Server.Listen.Socket, err)
		}
	} else if config.Current().Server.TLS.Enabled {
		log.Info("Starting HTTPS listener")
		tlsConfig, err := ssl.NewTLSConfig(config.Current().Server.TLS.CAFile, config.Current().Server.TLS.MutualTLS)
		if err != nil {
			return fmt.Errorf("create HTTP TLS configuration: %w", err)
		}
		tlsConfig.InsecureSkipVerify = config.Current().Server.TLS.SkipVerify
		if err = ssl.AppendKeyPairWithPassword(tlsConfig, config.Current().Server.TLS.CertFile, config.Current().Server.TLS.PrivateKeyFile, sslPEMPassword); err != nil {
			return fmt.Errorf("load HTTP TLS key pair: %w", err)
		}
		if err = serveTLSContext(ctx, config.Current().Server.Listen.Address, m, tlsConfig); err != nil {
			return fmt.Errorf("serve HTTPS on %s: %w", config.Current().Server.Listen.Address, err)
		}
	} else {
		log.Infof("Starting HTTP listener on %+v", config.Current().Server.Listen.Address)
		if err := listenHTTPContext(ctx, config.Current().Server.Listen.Address, m); err != nil {
			return fmt.Errorf("serve HTTP on %s: %w", config.Current().Server.Listen.Address, err)
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
	if config.Current().Agents.TLS.Enabled {
		log.Info("Starting agent HTTPS listener")
		tlsConfig, err := ssl.NewTLSConfig(config.Current().Agents.TLS.CAFile, config.Current().Agents.TLS.MutualTLS)
		if err != nil {
			return fmt.Errorf("create agent HTTP TLS configuration: %w", err)
		}
		tlsConfig.InsecureSkipVerify = config.Current().Agents.TLS.SkipVerify
		if err = ssl.AppendKeyPairWithPassword(tlsConfig, config.Current().Agents.TLS.CertFile, config.Current().Agents.TLS.PrivateKeyFile, agentSSLPEMPassword); err != nil {
			return fmt.Errorf("load agent HTTP TLS key pair: %w", err)
		}
		if err = ssl.ListenAndServeTLS(config.Current().Agents.ServerPort, m, tlsConfig); err != nil {
			return fmt.Errorf("serve agent HTTPS on %s: %w", config.Current().Agents.ServerPort, err)
		}
	} else {
		log.Info("Starting agent HTTP listener")
		if err := nethttp.ListenAndServe(config.Current().Agents.ServerPort, m); err != nil {
			return fmt.Errorf("serve agent HTTP on %s: %w", config.Current().Agents.ServerPort, err)
		}
	}
	log.Info("Agent server started")
	return nil
}

func newStandardHTTPRouter(manual *discovery.Manual) (*transport.Router, error) {
	if strings.EqualFold(config.Current().Authentication.Method, "basic") && config.Current().Authentication.Basic.User == "" {
		// Still allowed; may be disallowed in future versions.
		log.Warning("authentication.method is configured as 'basic' but authentication.basic.user is undefined. Running without authentication.")
	}

	options := transport.RouterOptions{
		Authentication: transport.AuthenticationOptions{
			Method:   config.Current().Authentication.Method,
			Username: config.Current().Authentication.Basic.User,
			Password: config.Current().Authentication.Basic.Password,
		},
		EnableGzip: true,
	}
	if config.Current().Server.TLS.MutualTLS {
		options.VerifyRequest = ssl.VerifyOUs(config.Current().Server.TLS.ValidOUs)
	}

	router, err := transport.NewRouter(options)
	if err != nil {
		return nil, err
	}
	assets, err := fs.Sub(webassets.Files(), "assets")
	if err != nil {
		return nil, fmt.Errorf("open embedded web assets: %w", err)
	}
	router.StaticFS(config.Current().Server.URLPrefix+"/web/assets", nethttp.FS(assets))

	api := http.Routes{URLPrefix: config.Current().Server.URLPrefix, InstanceAPI: instanceapi.API{Manual: manual}}
	web := httpweb.New(config.Current().Server.URLPrefix, nil)
	httpobservability.Register(router, config.Current().Server.URLPrefix)
	api.RegisterRequests(router)
	web.RegisterRequests(router)
	return router, nil
}

func newAgentsHTTPRouter() (*transport.Router, error) {
	options := transport.RouterOptions{EnableGzip: true}
	if config.Current().Agents.TLS.MutualTLS {
		options.VerifyRequest = ssl.VerifyOUs(config.Current().Agents.TLS.ValidOUs)
	}
	router, err := transport.NewRouter(options)
	if err != nil {
		return nil, err
	}
	agentAPI := httpagent.RegistrationAPI{URLPrefix: config.Current().Server.URLPrefix}
	agentAPI.RegisterRequests(router)
	return router, nil
}

// The application context owns both listener shutdown and manual discoveries.
func serveHTTPContext(ctx context.Context, listener net.Listener, handler nethttp.Handler) error {
	server := &nethttp.Server{Handler: handler, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	err := server.Serve(listener)
	close(done)
	<-stopped
	if errors.Is(err, nethttp.ErrServerClosed) && ctx.Err() != nil {
		return nil
	}
	return err
}
func listenHTTPContext(ctx context.Context, address string, handler nethttp.Handler) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	return serveHTTPContext(ctx, listener, handler)
}
func serveTLSContext(ctx context.Context, address string, handler nethttp.Handler, tlsConfig *tls.Config) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	return serveHTTPContext(ctx, tls.NewListener(listener, tlsConfig), handler)
}
