/*
   Copyright 2017 Shlomi Noach, GitHub Inc.

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

package orcraft

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/ssl"

	"github.com/openark/orchestrator/internal/golib/log"
)

var (
	httpClient    *http.Client
	httpTransport *http.Transport
)

func GetRaftHttpTransport() (*http.Transport, error) {
	// Checks whether there is a cached httpTransport to return:
	if httpTransport != nil {
		return httpTransport, nil
	}
	httpTimeout := 5 * time.Second
	dialer := &net.Dialer{Timeout: httpTimeout}

	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.Config.Server.TLS.SkipVerify,
	}
	if config.Config.Server.TLS.Enabled {
		caPool, err := ssl.ReadCAFile(config.Config.Server.TLS.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs = caPool

		if config.Config.Server.TLS.MutualTLS {
			var sslPEMPassword []byte
			if ssl.IsEncryptedPEM(config.Config.Server.TLS.PrivateKeyFile) {
				sslPEMPassword = ssl.GetPEMPassword(config.Config.Server.TLS.PrivateKeyFile)
			}
			if err := ssl.AppendKeyPairWithPassword(tlsConfig, config.Config.Server.TLS.CertFile, config.Config.Server.TLS.PrivateKeyFile, sslPEMPassword); err != nil {
				return nil, err
			}
		}
	}

	transport := &http.Transport{
		// API GET requests can mutate topology; never replay on a stale pooled connection.
		DisableKeepAlives:     true,
		TLSClientConfig:       tlsConfig,
		DialContext:           dialer.DialContext,
		ResponseHeaderTimeout: httpTimeout,
	}
	return transport, nil
}

func setupHttpClient() error {
	transport, err := GetRaftHttpTransport()
	if err != nil {
		return err
	}
	httpTransport = transport
	httpClient = &http.Client{Transport: httpTransport}

	return nil
}

func HttpGetLeader(path string) (response []byte, err error) {
	leaderURI := LeaderURI.Get()
	if leaderURI == "" {
		return nil, fmt.Errorf("raft leader URI unknown")
	}
	leaderAPI := leaderURI
	if config.Config.Server.URLPrefix != "" {
		// We know URLPrefix begind with "/"
		leaderAPI = fmt.Sprintf("%s%s", leaderAPI, config.Config.Server.URLPrefix)
	}
	leaderAPI = fmt.Sprintf("%s/api", leaderAPI)

	url := fmt.Sprintf("%s/%s", leaderAPI, path)

	req, err := http.NewRequest("GET", url, nil)
	switch strings.ToLower(config.Config.Authentication.Method) {
	case "basic", "multi":
		req.SetBasicAuth(config.Config.Authentication.Basic.User, config.Config.Authentication.Basic.Password)
	}

	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return body, log.Errorf("HttpGetLeader: got %d status on %s", res.StatusCode, url)
	}

	return body, nil
}
