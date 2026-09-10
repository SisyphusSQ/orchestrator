package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ConsulEndpoint is the normalized Consul HTTP endpoint used by the KV client factory.
type ConsulEndpoint struct {
	Address string // host:port or [ipv6]:port; never includes a scheme
	Scheme  string // http or https
}

func (cfg *Configuration) normalizeAndValidateConsul() error {
	provider, err := NormalizeConsulKVStoreProvider(cfg.Consul.KV.Provider)
	if err != nil {
		return err
	}
	cfg.Consul.KV.Provider = provider

	if cfg.Consul.HTTPTimeoutSeconds < 0 {
		return fmt.Errorf("consul.httpTimeoutSeconds must be >= 0")
	}

	certSet := strings.TrimSpace(cfg.Consul.TLS.CertFile) != ""
	keySet := strings.TrimSpace(cfg.Consul.TLS.PrivateKeyFile) != ""
	if certSet != keySet {
		return fmt.Errorf("consul.tls.certFile and consul.tls.privateKeyFile must both be set")
	}

	address := strings.TrimSpace(cfg.Consul.Address)
	if address == "" {
		if cfg.Consul.KV.CrossDataCenterDistribution {
			return fmt.Errorf("consul.kv.crossDataCenterDistribution requires consul.address")
		}
		if consulTLSOptionsConfigured(cfg) {
			return fmt.Errorf("consul.tls options require an https consul.address")
		}
		if scheme := strings.TrimSpace(cfg.Consul.Scheme); scheme != "" {
			if _, err := normalizeConsulScheme(scheme); err != nil {
				return err
			}
		}
		return nil
	}

	endpoint, err := NormalizeConsulEndpoint(address, cfg.Consul.Scheme)
	if err != nil {
		return err
	}
	// Keep an address-embedded scheme intact. Read and Reload apply multiple
	// configuration files in sequence; stripping it here would let a later
	// ConsulScheme value override the address on the next adjustment pass.
	cfg.Consul.Address = address
	cfg.Consul.Scheme = endpoint.Scheme

	if consulTLSOptionsConfigured(cfg) && endpoint.Scheme != "https" {
		return fmt.Errorf("consul TLS options require https")
	}
	return nil
}

func consulTLSOptionsConfigured(configuration *Configuration) bool {
	return strings.TrimSpace(configuration.Consul.TLS.CAFile) != "" ||
		strings.TrimSpace(configuration.Consul.TLS.CAPath) != "" ||
		strings.TrimSpace(configuration.Consul.TLS.CertFile) != "" ||
		strings.TrimSpace(configuration.Consul.TLS.PrivateKeyFile) != "" ||
		strings.TrimSpace(configuration.Consul.TLS.ServerName) != "" ||
		configuration.Consul.TLS.SkipVerify
}

// NormalizeConsulEndpoint accepts host:port or http[s]://host:port.
// When the address includes a scheme, that scheme wins over ConsulScheme.
func NormalizeConsulEndpoint(address, scheme string) (ConsulEndpoint, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		return ConsulEndpoint{}, fmt.Errorf("consul.address is empty")
	}

	if !strings.Contains(address, "://") {
		defaultScheme := "http"
		if strings.TrimSpace(scheme) != "" {
			normalizedScheme, err := normalizeConsulScheme(scheme)
			if err != nil {
				return ConsulEndpoint{}, err
			}
			defaultScheme = normalizedScheme
		}
		if err := rejectConsulAddressExtras(address); err != nil {
			return ConsulEndpoint{}, err
		}
		if err := validateConsulHostPort(address); err != nil {
			return ConsulEndpoint{}, err
		}
		return ConsulEndpoint{Address: address, Scheme: defaultScheme}, nil
	}

	parsed, err := url.Parse(address)
	if err != nil {
		return ConsulEndpoint{}, fmt.Errorf("consul.address has invalid URL syntax")
	}
	normalizedScheme, err := normalizeConsulScheme(parsed.Scheme)
	if err != nil {
		return ConsulEndpoint{}, err
	}
	if parsed.User != nil {
		return ConsulEndpoint{}, fmt.Errorf("consul.address must not include userinfo")
	}
	if parsed.Path != "" || parsed.RawPath != "" {
		return ConsulEndpoint{}, fmt.Errorf("consul.address must not include a path")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		return ConsulEndpoint{}, fmt.Errorf("consul.address must not include a query")
	}
	if parsed.Fragment != "" || strings.Contains(address, "#") {
		return ConsulEndpoint{}, fmt.Errorf("consul.address must not include a fragment")
	}
	host := parsed.Host
	if host == "" {
		return ConsulEndpoint{}, fmt.Errorf("consul.address is missing host")
	}
	if err := validateConsulHostPort(host); err != nil {
		return ConsulEndpoint{}, err
	}
	return ConsulEndpoint{Address: host, Scheme: normalizedScheme}, nil
}

// NormalizeConsulKVStoreProvider accepts consul, consul-txn, and the historical alias consul_txn.
func NormalizeConsulKVStoreProvider(provider string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "consul":
		return "consul", nil
	case "consul-txn", "consul_txn":
		return "consul-txn", nil
	default:
		return "", fmt.Errorf("consul.kv.provider %q is not supported; use consul or consul-txn", provider)
	}
}

func normalizeConsulScheme(scheme string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "http", "https":
		return strings.ToLower(strings.TrimSpace(scheme)), nil
	default:
		return "", fmt.Errorf("consul scheme %q is not supported; only http and https are allowed", scheme)
	}
}

func rejectConsulAddressExtras(address string) error {
	if strings.Contains(address, "@") {
		return fmt.Errorf("consul.address must not include userinfo")
	}
	if strings.ContainsAny(address, "/?#") {
		return fmt.Errorf("consul.address must not include a path, query, or fragment")
	}
	return nil
}

func validateConsulHostPort(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("consul.address must be host:port or http[s]://host:port")
	}
	if strings.TrimSpace(host) == "" {
		return fmt.Errorf("consul.address is missing host")
	}
	if strings.TrimSpace(port) == "" {
		return fmt.Errorf("consul.address is missing port")
	}
	return nil
}
