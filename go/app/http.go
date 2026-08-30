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
	"fmt"
	"net"
	nethttp "net/http"
	"strings"
	"time"

	"github.com/openark/orchestrator/go/agent"
	"github.com/openark/orchestrator/go/collection"
	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/http"
	"github.com/openark/orchestrator/go/inst"
	"github.com/openark/orchestrator/go/logic"
	"github.com/openark/orchestrator/go/process"
	"github.com/openark/orchestrator/go/ssl"

	"github.com/go-martini/martini"
	"github.com/martini-contrib/auth"
	"github.com/martini-contrib/gzip"
	"github.com/martini-contrib/render"
	"github.com/openark/golib/log"
)

const discoveryMetricsName = "DISCOVERY_METRICS"

var sslPEMPassword []byte
var agentSSLPEMPassword []byte
var discoveryMetrics *collection.Collection

// Http starts serving
func Http(continuousDiscovery bool) error {
	promptForSSLPasswords()
	process.ContinuousRegistration(process.OrchestratorExecutionHttpMode, "")

	martini.Env = martini.Prod
	runtimeErrors := make(chan error, 3)
	go reportRuntimeError(runtimeErrors, "standard HTTP server", func() error {
		return standardHttp(continuousDiscovery, runtimeErrors)
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
func standardHttp(continuousDiscovery bool, runtimeErrors chan<- error) error {
	m := martini.Classic()

	switch strings.ToLower(config.Config.AuthenticationMethod) {
	case "basic":
		{
			if config.Config.HTTPAuthUser == "" {
				// Still allowed; may be disallowed in future versions
				log.Warning("AuthenticationMethod is configured as 'basic' but HTTPAuthUser undefined. Running without authentication.")
			}
			m.Use(auth.Basic(config.Config.HTTPAuthUser, config.Config.HTTPAuthPassword))
		}
	case "multi":
		{
			if config.Config.HTTPAuthUser == "" {
				// Still allowed; may be disallowed in future versions
				return fmt.Errorf("AuthenticationMethod is 'multi' but HTTPAuthUser is undefined")
			}

			m.Use(auth.BasicFunc(func(username, password string) bool {
				if username == "readonly" {
					// Will be treated as "read-only"
					return true
				}
				return auth.SecureCompare(username, config.Config.HTTPAuthUser) && auth.SecureCompare(password, config.Config.HTTPAuthPassword)
			}))
		}
	default:
		{
			// We inject a dummy User object because we have function signatures with User argument in api.go
			m.Map(auth.User(""))
		}
	}

	m.Use(gzip.All())
	// Render html templates from templates directory
	m.Use(render.Renderer(render.Options{
		Directory:       "resources",
		Layout:          "templates/layout",
		HTMLContentType: "text/html",
	}))
	m.Use(martini.Static("resources/public", martini.StaticOptions{Prefix: config.Config.URLPrefix}))
	if config.Config.UseMutualTLS {
		m.Use(ssl.VerifyOUs(config.Config.SSLValidOUs))
	}

	inst.SetMaintenanceOwner(process.ThisHostname)

	if continuousDiscovery {
		// start to expire metric collection info
		discoveryMetrics = collection.CreateOrReturnCollection(discoveryMetricsName)
		discoveryMetrics.SetExpirePeriod(time.Duration(config.Config.DiscoveryCollectionRetentionSeconds) * time.Second)

		log.Info("Starting Discovery")
		go reportRuntimeError(runtimeErrors, "continuous discovery", logic.ContinuousDiscovery)
	}

	log.Info("Registering endpoints")
	http.API.URLPrefix = config.Config.URLPrefix
	http.Web.URLPrefix = config.Config.URLPrefix
	http.API.RegisterRequests(m)
	http.Web.RegisterRequests(m)

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
	m := martini.Classic()
	m.Use(gzip.All())
	m.Use(render.Renderer())
	if config.Config.AgentsUseMutualTLS {
		m.Use(ssl.VerifyOUs(config.Config.AgentSSLValidOUs))
	}

	log.Info("Starting agents listener")

	agent.InitHttpClient()
	go logic.ContinuousAgentsPoll()

	http.AgentsAPI.URLPrefix = config.Config.URLPrefix
	http.AgentsAPI.RegisterRequests(m)

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
