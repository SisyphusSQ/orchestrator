package config

import (
	"strings"
	"testing"
)

func TestNormalizeConsulEndpoint(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		address         string
		scheme          string
		wantAddress     string
		wantScheme      string
		wantErrContains string
	}{
		{
			name:        "host-port uses configured http scheme",
			address:     "127.0.0.1:8500",
			scheme:      "http",
			wantAddress: "127.0.0.1:8500",
			wantScheme:  "http",
		},
		{
			name:        "host-port uses configured https scheme",
			address:     "consul.example.com:8501",
			scheme:      "HTTPS",
			wantAddress: "consul.example.com:8501",
			wantScheme:  "https",
		},
		{
			name:        "address scheme wins over configured scheme",
			address:     "https://127.0.0.1:8501",
			scheme:      "http",
			wantAddress: "127.0.0.1:8501",
			wantScheme:  "https",
		},
		{
			name:        "address scheme ignores invalid configured scheme",
			address:     "https://127.0.0.1:8501",
			scheme:      "ftp",
			wantAddress: "127.0.0.1:8501",
			wantScheme:  "https",
		},
		{
			name:        "http url keeps host and port",
			address:     "http://127.0.0.1:8500",
			scheme:      "https",
			wantAddress: "127.0.0.1:8500",
			wantScheme:  "http",
		},
		{
			name:        "ipv6 url",
			address:     "https://[::1]:8501",
			scheme:      "http",
			wantAddress: "[::1]:8501",
			wantScheme:  "https",
		},
		{
			name:            "rejects userinfo",
			address:         "http://user:pass@127.0.0.1:8500",
			scheme:          "http",
			wantErrContains: "userinfo",
		},
		{
			name:            "rejects path",
			address:         "http://127.0.0.1:8500/v1",
			scheme:          "http",
			wantErrContains: "path",
		},
		{
			name:            "rejects root path",
			address:         "http://127.0.0.1:8500/",
			scheme:          "http",
			wantErrContains: "path",
		},
		{
			name:            "rejects query",
			address:         "http://127.0.0.1:8500?dc=east",
			scheme:          "http",
			wantErrContains: "query",
		},
		{
			name:            "rejects fragment",
			address:         "http://127.0.0.1:8500#frag",
			scheme:          "http",
			wantErrContains: "fragment",
		},
		{
			name:            "rejects ftp scheme",
			address:         "ftp://127.0.0.1:8500",
			scheme:          "http",
			wantErrContains: "http and https",
		},
		{
			name:            "rejects unix scheme",
			address:         "unix:///tmp/consul.sock",
			scheme:          "http",
			wantErrContains: "http and https",
		},
		{
			name:            "rejects configured unknown scheme",
			address:         "127.0.0.1:8500",
			scheme:          "ftp",
			wantErrContains: "ftp",
		},
		{
			name:            "rejects host-port with path",
			address:         "127.0.0.1:8500/v1",
			scheme:          "http",
			wantErrContains: "path",
		},
		{
			name:            "rejects host without port",
			address:         "consul.example.com",
			scheme:          "http",
			wantErrContains: "host:port",
		},
		{
			name:            "rejects url without port",
			address:         "https://consul.example.com",
			scheme:          "http",
			wantErrContains: "host:port",
		},
		{
			name:            "rejects empty url host",
			address:         "https://:8501",
			scheme:          "http",
			wantErrContains: "missing host",
		},
		{
			name:            "rejects empty address",
			address:         "",
			scheme:          "http",
			wantErrContains: "empty",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			endpoint, err := NormalizeConsulEndpoint(testCase.address, testCase.scheme)
			if testCase.wantErrContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", testCase.wantErrContains)
				}
				if !strings.Contains(err.Error(), testCase.wantErrContains) {
					t.Fatalf("error %q does not contain %q", err, testCase.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeConsulEndpoint() error: %v", err)
			}
			if endpoint.Address != testCase.wantAddress || endpoint.Scheme != testCase.wantScheme {
				t.Fatalf("got %+v, want address %q scheme %q", endpoint, testCase.wantAddress, testCase.wantScheme)
			}
		})
	}
}

func TestNormalizeConsulKVStoreProvider(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: "consul"},
		{in: "consul", want: "consul"},
		{in: "CONSUL", want: "consul"},
		{in: "consul-txn", want: "consul-txn"},
		{in: "consul_txn", want: "consul-txn"},
		{in: "Consul_Txn", want: "consul-txn"},
		{in: "etcd", wantErr: true},
		{in: "consul-watch", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.in, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeConsulKVStoreProvider(testCase.in)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("got %q, want %q", got, testCase.want)
			}
		})
	}
}

func TestPostReadAdjustmentsConsulContract(t *testing.T) {
	testCases := []struct {
		name            string
		mutate          func(*Configuration)
		wantAddress     string
		wantScheme      string
		wantProvider    string
		wantTimeout     int
		wantErrContains string
	}{
		{
			name: "preserves url address and applies embedded scheme",
			mutate: func(c *Configuration) {
				c.Consul.Address = "https://consul.example.com:8501"
				c.Consul.Scheme = "http"
			},
			wantAddress:  "https://consul.example.com:8501",
			wantScheme:   "https",
			wantProvider: "consul",
			wantTimeout:  60,
		},
		{
			name: "normalizes historical provider alias",
			mutate: func(c *Configuration) {
				c.Consul.KV.Provider = "consul_txn"
			},
			wantScheme:   "http",
			wantProvider: "consul-txn",
			wantTimeout:  60,
		},
		{
			name: "keeps explicit zero timeout",
			mutate: func(c *Configuration) {
				c.Consul.Address = "127.0.0.1:8500"
				c.Consul.HTTPTimeoutSeconds = 0
			},
			wantAddress:  "127.0.0.1:8500",
			wantScheme:   "http",
			wantProvider: "consul",
			wantTimeout:  0,
		},
		{
			name: "rejects negative timeout",
			mutate: func(c *Configuration) {
				c.Consul.HTTPTimeoutSeconds = -1
			},
			wantErrContains: "consul.httpTimeoutSeconds",
		},
		{
			name: "rejects unknown provider",
			mutate: func(c *Configuration) {
				c.Consul.KV.Provider = "vault"
			},
			wantErrContains: "consul.kv.provider",
		},
		{
			name: "rejects tls options on http",
			mutate: func(c *Configuration) {
				c.Consul.Address = "127.0.0.1:8500"
				c.Consul.TLS.CAFile = "/tmp/ca.pem"
			},
			wantErrContains: "https",
		},
		{
			name: "rejects skip verify on http",
			mutate: func(c *Configuration) {
				c.Consul.Address = "http://127.0.0.1:8500"
				c.Consul.TLS.SkipVerify = true
			},
			wantErrContains: "https",
		},
		{
			name: "rejects unpaired client cert",
			mutate: func(c *Configuration) {
				c.Consul.Address = "https://127.0.0.1:8501"
				c.Consul.TLS.CertFile = "/tmp/client.pem"
			},
			wantErrContains: "both be set",
		},
		{
			name: "rejects unpaired client key",
			mutate: func(c *Configuration) {
				c.Consul.Address = "https://127.0.0.1:8501"
				c.Consul.TLS.PrivateKeyFile = "/tmp/client.key"
			},
			wantErrContains: "both be set",
		},
		{
			name: "allows paired certs on https",
			mutate: func(c *Configuration) {
				c.Consul.Address = "https://127.0.0.1:8501"
				c.Consul.TLS.CertFile = "/tmp/client.pem"
				c.Consul.TLS.PrivateKeyFile = "/tmp/client.key"
				c.Consul.TLS.ServerName = "consul.example.com"
			},
			wantAddress:  "https://127.0.0.1:8501",
			wantScheme:   "https",
			wantProvider: "consul",
			wantTimeout:  60,
		},
		{
			name: "rejects cross dc without address",
			mutate: func(c *Configuration) {
				c.Consul.Address = ""
				c.Consul.KV.CrossDataCenterDistribution = true
			},
			wantErrContains: "consul.kv.crossDataCenterDistribution",
		},
		{
			name: "rejects tls options without address",
			mutate: func(c *Configuration) {
				c.Consul.TLS.CAFile = "/tmp/ca.pem"
			},
			wantErrContains: "https",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			configuration := newConfiguration()
			testCase.mutate(configuration)
			err := configuration.postReadAdjustments()
			if testCase.wantErrContains != "" {
				if err == nil {
					t.Fatalf("expected error containing %q", testCase.wantErrContains)
				}
				if !strings.Contains(err.Error(), testCase.wantErrContains) {
					t.Fatalf("error %q does not contain %q", err, testCase.wantErrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("postReadAdjustments() error: %v", err)
			}
			if configuration.Consul.Address != testCase.wantAddress {
				t.Fatalf("ConsulAddress = %q, want %q", configuration.Consul.Address, testCase.wantAddress)
			}
			if configuration.Consul.Scheme != testCase.wantScheme {
				t.Fatalf("ConsulScheme = %q, want %q", configuration.Consul.Scheme, testCase.wantScheme)
			}
			if configuration.Consul.KV.Provider != testCase.wantProvider {
				t.Fatalf("ConsulKVStoreProvider = %q, want %q", configuration.Consul.KV.Provider, testCase.wantProvider)
			}
			if configuration.Consul.HTTPTimeoutSeconds != testCase.wantTimeout {
				t.Fatalf("ConsulHttpTimeoutSeconds = %d, want %d", configuration.Consul.HTTPTimeoutSeconds, testCase.wantTimeout)
			}
		})
	}
}

func TestPostReadAdjustmentsEmbeddedSchemeSurvivesLaterAdjustment(t *testing.T) {
	configuration := newConfiguration()
	configuration.Consul.Address = "https://consul.example.com:8501"
	configuration.Consul.Scheme = "http"
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("first postReadAdjustments() error: %v", err)
	}

	configuration.Consul.Scheme = "http"
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("second postReadAdjustments() error: %v", err)
	}
	if configuration.Consul.Scheme != "https" {
		t.Fatalf("ConsulScheme = %q after later adjustment, want embedded https", configuration.Consul.Scheme)
	}
}

func TestNormalizeConsulEndpointErrorsDoNotLeakAddressSecrets(t *testing.T) {
	t.Parallel()

	for _, address := range []string{
		"https://user:super-secret@consul.example.com:8501",
		"https://consul.example.com:8501?token=super-secret",
	} {
		_, err := NormalizeConsulEndpoint(address, "http")
		if err == nil {
			t.Fatalf("NormalizeConsulEndpoint(%q) returned nil error", address)
		}
		if strings.Contains(err.Error(), "super-secret") {
			t.Fatalf("error leaked address secret: %q", err)
		}
	}
}

func TestForceReadConsulDefaultsAndValidation(t *testing.T) {
	previous := *Config
	previousReadFileNames := append([]string(nil), readFileNames...)
	t.Cleanup(func() {
		*Config = previous
		readFileNames = previousReadFileNames
	})

	t.Run("defaults", func(t *testing.T) {
		_, err := ForceRead(writeConfigFixture(t, `{"logging":{"debug":true}}`))
		if err != nil {
			t.Fatalf("ForceRead() error: %v", err)
		}
		if Config.Consul.Address != "" {
			t.Fatalf("ConsulAddress = %q, want empty", Config.Consul.Address)
		}
		if Config.Consul.Scheme != "http" {
			t.Fatalf("ConsulScheme = %q, want http", Config.Consul.Scheme)
		}
		if Config.Consul.ACLToken != "" {
			t.Fatalf("ConsulAclToken = %q, want empty", Config.Consul.ACLToken)
		}
		if Config.Consul.Datacenter != "" {
			t.Fatalf("ConsulDatacenter = %q, want empty", Config.Consul.Datacenter)
		}
		if Config.Consul.TLS.SkipVerify {
			t.Fatal("ConsulTLSSkipVerify default is true; want false")
		}
		if Config.Consul.HTTPTimeoutSeconds != 60 {
			t.Fatalf("ConsulHttpTimeoutSeconds = %d, want 60", Config.Consul.HTTPTimeoutSeconds)
		}
		if Config.Consul.KV.Provider != "consul" {
			t.Fatalf("ConsulKVStoreProvider = %q, want consul", Config.Consul.KV.Provider)
		}
	})

	t.Run("explicit zero timeout", func(t *testing.T) {
		_, err := ForceRead(writeConfigFixture(t, `{"consul":{"address":"127.0.0.1:8500","httpTimeoutSeconds":0}}`))
		if err != nil {
			t.Fatalf("ForceRead() error: %v", err)
		}
		if Config.Consul.HTTPTimeoutSeconds != 0 {
			t.Fatalf("ConsulHttpTimeoutSeconds = %d, want 0", Config.Consul.HTTPTimeoutSeconds)
		}
	})

	t.Run("rejects unknown provider", func(t *testing.T) {
		_, err := ForceRead(writeConfigFixture(t, `{"consul":{"kv":{"provider":"zk"}}}`))
		if err == nil {
			t.Fatal("expected unknown provider to fail")
		}
		if !strings.Contains(err.Error(), "consul.kv.provider") {
			t.Fatalf("error %q does not mention consul.kv.provider", err)
		}
	})
}

func TestConsulMaxKVsPerTransactionNormalizationStillApplies(t *testing.T) {
	configuration := newConfiguration()
	configuration.Consul.KV.MaxKVsPerTransaction = 1
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("postReadAdjustments() error: %v", err)
	}
	if configuration.Consul.KV.MaxKVsPerTransaction != ConsulKVsPerCluster {
		t.Fatalf("got %d, want %d", configuration.Consul.KV.MaxKVsPerTransaction, ConsulKVsPerCluster)
	}

	configuration = newConfiguration()
	configuration.Consul.KV.MaxKVsPerTransaction = 100
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("postReadAdjustments() error: %v", err)
	}
	if configuration.Consul.KV.MaxKVsPerTransaction != ConsulMaxTransactionOps {
		t.Fatalf("got %d, want %d", configuration.Consul.KV.MaxKVsPerTransaction, ConsulMaxTransactionOps)
	}
}
