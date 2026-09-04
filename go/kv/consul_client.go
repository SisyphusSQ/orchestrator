package kv

import (
	"fmt"
	"strings"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-cleanhttp"

	"github.com/openark/golib/log"
	"github.com/openark/orchestrator/go/config"
)

const consulTLSSkipVerifyWarning = "Consul TLS certificate verification is disabled by ConsulTLSSkipVerify"

// consulClientOptions is the project-owned Consul client contract. The config
// package and KVStore interface do not expose HashiCorp SDK types.
type consulClientOptions struct {
	Address            string
	Scheme             string
	Token              string
	Datacenter         string
	TLSCAFile          string
	TLSCAPath          string
	TLSCertFile        string
	TLSPrivateKeyFile  string
	TLSServerName      string
	TLSSkipVerify      bool
	HTTPTimeoutSeconds int
}

func consulClientOptionsFromConfig(cfg *config.Configuration) consulClientOptions {
	return consulClientOptions{
		Address:            cfg.ConsulAddress,
		Scheme:             cfg.ConsulScheme,
		Token:              cfg.ConsulAclToken,
		Datacenter:         cfg.ConsulDatacenter,
		TLSCAFile:          cfg.ConsulTLSCAFile,
		TLSCAPath:          cfg.ConsulTLSCAPath,
		TLSCertFile:        cfg.ConsulTLSCertFile,
		TLSPrivateKeyFile:  cfg.ConsulTLSPrivateKeyFile,
		TLSServerName:      cfg.ConsulTLSServerName,
		TLSSkipVerify:      cfg.ConsulTLSSkipVerify,
		HTTPTimeoutSeconds: cfg.ConsulHttpTimeoutSeconds,
	}
}

func newConsulClientFromConfig(cfg *config.Configuration) (*consulapi.Client, error) {
	return newConsulClient(consulClientOptionsFromConfig(cfg))
}

// newConsulClient builds a pooled official Consul API client. TLS behavior is
// taken from JSON options and system roots: a prebuilt HttpClient prevents
// CONSUL_HTTP_SSL_VERIFY from silently switching the client to insecure.
// Empty Address returns (nil, nil) so callers can skip the external store.
func newConsulClient(opts consulClientOptions) (*consulapi.Client, error) {
	if strings.TrimSpace(opts.Address) == "" {
		return nil, nil
	}

	endpoint, err := config.NormalizeConsulEndpoint(opts.Address, opts.Scheme)
	if err != nil {
		return nil, fmt.Errorf("create consul client: %w", err)
	}

	if opts.TLSSkipVerify {
		log.Warning(consulTLSSkipVerifyWarning)
	}

	tlsConf := consulapi.TLSConfig{
		Address:            opts.TLSServerName,
		CAFile:             opts.TLSCAFile,
		CAPath:             opts.TLSCAPath,
		CertFile:           opts.TLSCertFile,
		KeyFile:            opts.TLSPrivateKeyFile,
		InsecureSkipVerify: opts.TLSSkipVerify,
	}

	transport := cleanhttp.DefaultPooledTransport()
	httpClient, err := consulapi.NewHttpClient(transport, tlsConf)
	if err != nil {
		return nil, fmt.Errorf("create consul http client: %w", err)
	}
	if opts.HTTPTimeoutSeconds > 0 {
		httpClient.Timeout = time.Duration(opts.HTTPTimeoutSeconds) * time.Second
	}

	clientConfig := &consulapi.Config{
		Address:    endpoint.Address,
		Scheme:     endpoint.Scheme,
		Datacenter: opts.Datacenter,
		Token:      opts.Token,
		Transport:  transport,
		HttpClient: httpClient,
		TLSConfig:  tlsConf,
	}
	client, err := consulapi.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("create consul client: %w", err)
	}
	return client, nil
}
