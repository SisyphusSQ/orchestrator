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
		testCase := testCase
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
		testCase := testCase
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
				c.ConsulAddress = "https://consul.example.com:8501"
				c.ConsulScheme = "http"
			},
			wantAddress:  "https://consul.example.com:8501",
			wantScheme:   "https",
			wantProvider: "consul",
			wantTimeout:  60,
		},
		{
			name: "normalizes historical provider alias",
			mutate: func(c *Configuration) {
				c.ConsulKVStoreProvider = "consul_txn"
			},
			wantScheme:   "http",
			wantProvider: "consul-txn",
			wantTimeout:  60,
		},
		{
			name: "keeps explicit zero timeout",
			mutate: func(c *Configuration) {
				c.ConsulAddress = "127.0.0.1:8500"
				c.ConsulHttpTimeoutSeconds = 0
			},
			wantAddress:  "127.0.0.1:8500",
			wantScheme:   "http",
			wantProvider: "consul",
			wantTimeout:  0,
		},
		{
			name: "rejects negative timeout",
			mutate: func(c *Configuration) {
				c.ConsulHttpTimeoutSeconds = -1
			},
			wantErrContains: "ConsulHttpTimeoutSeconds",
		},
		{
			name: "rejects unknown provider",
			mutate: func(c *Configuration) {
				c.ConsulKVStoreProvider = "vault"
			},
			wantErrContains: "ConsulKVStoreProvider",
		},
		{
			name: "rejects tls options on http",
			mutate: func(c *Configuration) {
				c.ConsulAddress = "127.0.0.1:8500"
				c.ConsulTLSCAFile = "/tmp/ca.pem"
			},
			wantErrContains: "https",
		},
		{
			name: "rejects skip verify on http",
			mutate: func(c *Configuration) {
				c.ConsulAddress = "http://127.0.0.1:8500"
				c.ConsulTLSSkipVerify = true
			},
			wantErrContains: "https",
		},
		{
			name: "rejects unpaired client cert",
			mutate: func(c *Configuration) {
				c.ConsulAddress = "https://127.0.0.1:8501"
				c.ConsulTLSCertFile = "/tmp/client.pem"
			},
			wantErrContains: "both be set",
		},
		{
			name: "rejects unpaired client key",
			mutate: func(c *Configuration) {
				c.ConsulAddress = "https://127.0.0.1:8501"
				c.ConsulTLSPrivateKeyFile = "/tmp/client.key"
			},
			wantErrContains: "both be set",
		},
		{
			name: "allows paired certs on https",
			mutate: func(c *Configuration) {
				c.ConsulAddress = "https://127.0.0.1:8501"
				c.ConsulTLSCertFile = "/tmp/client.pem"
				c.ConsulTLSPrivateKeyFile = "/tmp/client.key"
				c.ConsulTLSServerName = "consul.example.com"
			},
			wantAddress:  "https://127.0.0.1:8501",
			wantScheme:   "https",
			wantProvider: "consul",
			wantTimeout:  60,
		},
		{
			name: "rejects cross dc without address",
			mutate: func(c *Configuration) {
				c.ConsulAddress = ""
				c.ConsulCrossDataCenterDistribution = true
			},
			wantErrContains: "ConsulCrossDataCenterDistribution",
		},
		{
			name: "rejects tls options without address",
			mutate: func(c *Configuration) {
				c.ConsulTLSCAFile = "/tmp/ca.pem"
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
			if configuration.ConsulAddress != testCase.wantAddress {
				t.Fatalf("ConsulAddress = %q, want %q", configuration.ConsulAddress, testCase.wantAddress)
			}
			if configuration.ConsulScheme != testCase.wantScheme {
				t.Fatalf("ConsulScheme = %q, want %q", configuration.ConsulScheme, testCase.wantScheme)
			}
			if configuration.ConsulKVStoreProvider != testCase.wantProvider {
				t.Fatalf("ConsulKVStoreProvider = %q, want %q", configuration.ConsulKVStoreProvider, testCase.wantProvider)
			}
			if configuration.ConsulHttpTimeoutSeconds != testCase.wantTimeout {
				t.Fatalf("ConsulHttpTimeoutSeconds = %d, want %d", configuration.ConsulHttpTimeoutSeconds, testCase.wantTimeout)
			}
		})
	}
}

func TestPostReadAdjustmentsEmbeddedSchemeSurvivesLaterAdjustment(t *testing.T) {
	configuration := newConfiguration()
	configuration.ConsulAddress = "https://consul.example.com:8501"
	configuration.ConsulScheme = "http"
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("first postReadAdjustments() error: %v", err)
	}

	configuration.ConsulScheme = "http"
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("second postReadAdjustments() error: %v", err)
	}
	if configuration.ConsulScheme != "https" {
		t.Fatalf("ConsulScheme = %q after later adjustment, want embedded https", configuration.ConsulScheme)
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
		_, err := ForceRead(writeConfigFixture(t, `{"Debug":true}`))
		if err != nil {
			t.Fatalf("ForceRead() error: %v", err)
		}
		if Config.ConsulAddress != "" {
			t.Fatalf("ConsulAddress = %q, want empty", Config.ConsulAddress)
		}
		if Config.ConsulScheme != "http" {
			t.Fatalf("ConsulScheme = %q, want http", Config.ConsulScheme)
		}
		if Config.ConsulAclToken != "" {
			t.Fatalf("ConsulAclToken = %q, want empty", Config.ConsulAclToken)
		}
		if Config.ConsulDatacenter != "" {
			t.Fatalf("ConsulDatacenter = %q, want empty", Config.ConsulDatacenter)
		}
		if Config.ConsulTLSSkipVerify {
			t.Fatal("ConsulTLSSkipVerify default is true; want false")
		}
		if Config.ConsulHttpTimeoutSeconds != 60 {
			t.Fatalf("ConsulHttpTimeoutSeconds = %d, want 60", Config.ConsulHttpTimeoutSeconds)
		}
		if Config.ConsulKVStoreProvider != "consul" {
			t.Fatalf("ConsulKVStoreProvider = %q, want consul", Config.ConsulKVStoreProvider)
		}
	})

	t.Run("explicit zero timeout", func(t *testing.T) {
		_, err := ForceRead(writeConfigFixture(t, `{"ConsulAddress":"127.0.0.1:8500","ConsulHttpTimeoutSeconds":0}`))
		if err != nil {
			t.Fatalf("ForceRead() error: %v", err)
		}
		if Config.ConsulHttpTimeoutSeconds != 0 {
			t.Fatalf("ConsulHttpTimeoutSeconds = %d, want 0", Config.ConsulHttpTimeoutSeconds)
		}
	})

	t.Run("rejects unknown provider", func(t *testing.T) {
		_, err := ForceRead(writeConfigFixture(t, `{"ConsulKVStoreProvider":"zk"}`))
		if err == nil {
			t.Fatal("expected unknown provider to fail")
		}
		if !strings.Contains(err.Error(), "ConsulKVStoreProvider") {
			t.Fatalf("error %q does not mention ConsulKVStoreProvider", err)
		}
	})
}

func TestConsulMaxKVsPerTransactionNormalizationStillApplies(t *testing.T) {
	configuration := newConfiguration()
	configuration.ConsulMaxKVsPerTransaction = 1
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("postReadAdjustments() error: %v", err)
	}
	if configuration.ConsulMaxKVsPerTransaction != ConsulKVsPerCluster {
		t.Fatalf("got %d, want %d", configuration.ConsulMaxKVsPerTransaction, ConsulKVsPerCluster)
	}

	configuration = newConfiguration()
	configuration.ConsulMaxKVsPerTransaction = 100
	if err := configuration.postReadAdjustments(); err != nil {
		t.Fatalf("postReadAdjustments() error: %v", err)
	}
	if configuration.ConsulMaxKVsPerTransaction != ConsulMaxTransactionOps {
		t.Fatalf("got %d, want %d", configuration.ConsulMaxKVsPerTransaction, ConsulMaxTransactionOps)
	}
}
