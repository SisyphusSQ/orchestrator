package kv

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"github.com/openark/orchestrator/go/config"
)

type tlsMaterial struct {
	CACertPEM      []byte
	ServerCertPEM  []byte
	ServerKeyPEM   []byte
	ClientCertPEM  []byte
	ClientKeyPEM   []byte
	OtherCACertPEM []byte
	ServerTLS      tls.Certificate
	ClientTLS      tls.Certificate
	CAPool         *x509.CertPool
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func encodeCert(certDER []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
}

func encodeKey(key *rsa.PrivateKey) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

func createCert(t *testing.T, template *x509.Certificate, parent *x509.Certificate, parentKey *rsa.PrivateKey) ([]byte, []byte, *x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key := mustRSAKey(t)
	if parent == nil {
		parent = template
		parentKey = key
	}
	der, err := x509.CreateCertificate(rand.Reader, template, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	return encodeCert(der), encodeKey(key), parsed, key
}

func newTLSMaterial(t *testing.T, serverName string) tlsMaterial {
	t.Helper()
	serial := func() *big.Int {
		n, err := rand.Int(rand.Reader, big.NewInt(1<<62))
		if err != nil {
			t.Fatalf("serial: %v", err)
		}
		return n
	}

	caTemplate := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "orchestrator-consul-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	caCertPEM, _, caCert, caKey := createCert(t, caTemplate, nil, nil)

	otherCATemplate := *caTemplate
	otherCATemplate.SerialNumber = serial()
	otherCATemplate.Subject = pkix.Name{CommonName: "other-ca"}
	otherCACertPEM, _, _, _ := createCert(t, &otherCATemplate, nil, nil)

	serverTemplate := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: serverName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{serverName, "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	serverCertPEM, serverKeyPEM, _, _ := createCert(t, serverTemplate, caCert, caKey)

	clientTemplate := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "orchestrator-consul-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientCertPEM, clientKeyPEM, _, _ := createCert(t, clientTemplate, caCert, caKey)

	serverTLS, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatalf("server key pair: %v", err)
	}
	clientTLS, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		t.Fatalf("client key pair: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCertPEM) {
		t.Fatal("append CA")
	}
	return tlsMaterial{
		CACertPEM:      caCertPEM,
		ServerCertPEM:  serverCertPEM,
		ServerKeyPEM:   serverKeyPEM,
		ClientCertPEM:  clientCertPEM,
		ClientKeyPEM:   clientKeyPEM,
		OtherCACertPEM: otherCACertPEM,
		ServerTLS:      serverTLS,
		ClientTLS:      clientTLS,
		CAPool:         pool,
	}
}

func writeTempPEM(t *testing.T, name string, pemBytes []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func kvPutOKHandler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, "/v1/kv/") {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.String())
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(true)
	})
}

func startConsulTLSServer(t *testing.T, handler http.Handler, material tlsMaterial, requireClient bool) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{material.ServerTLS},
		MinVersion:   tls.VersionTLS12,
	}
	if requireClient {
		server.TLS.ClientCAs = material.CAPool
		server.TLS.ClientAuth = tls.RequireAndVerifyClientCert
	}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

func putProbe(t *testing.T, client *consulapi.Client) error {
	t.Helper()
	_, err := client.KV().Put(&consulapi.KVPair{Key: "mysql/master/cluster", Value: []byte("mysql.example.com:3306")}, nil)
	return err
}

func TestConsulClientHTTPSPrivateCA(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSCAFile = writeTempPEM(t, "ca.pem", material.CACertPEM)
	config.Config.ConsulTLSServerName = "consul.test"

	client := mustConsulClient(t)
	if err := putProbe(t, client); err != nil {
		t.Fatalf("https with private CA: %v", err)
	}
}

func TestConsulClientHTTPSSystemRootsRejectPrivateCert(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSServerName = "consul.test"

	client := mustConsulClient(t)
	err := putProbe(t, client)
	if err == nil {
		t.Fatal("expected system roots to reject the private Consul certificate")
	}
}

func TestConsulClientHTTPSCAPath(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	configureConsulTest(t, server.URL, false)
	caDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(caDir, "ca.pem"), material.CACertPEM, 0o600); err != nil {
		t.Fatalf("write CA path: %v", err)
	}
	config.Config.ConsulTLSCAPath = caDir
	config.Config.ConsulTLSServerName = "consul.test"

	client := mustConsulClient(t)
	if err := putProbe(t, client); err != nil {
		t.Fatalf("https with CAPath: %v", err)
	}
}

func TestConsulClientServerNameMismatch(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSCAFile = writeTempPEM(t, "ca.pem", material.CACertPEM)
	config.Config.ConsulTLSServerName = "wrong.example"

	client := mustConsulClient(t)
	if err := putProbe(t, client); err == nil {
		t.Fatal("expected ServerName mismatch to fail")
	}
}

func TestConsulClientWrongCA(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSCAFile = writeTempPEM(t, "other-ca.pem", material.OtherCACertPEM)
	config.Config.ConsulTLSServerName = "consul.test"

	client := mustConsulClient(t)
	if err := putProbe(t, client); err == nil {
		t.Fatal("expected wrong CA to fail")
	}
}

func TestConsulClientMTLS(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, true)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSCAFile = writeTempPEM(t, "ca.pem", material.CACertPEM)
	config.Config.ConsulTLSCertFile = writeTempPEM(t, "client.pem", material.ClientCertPEM)
	config.Config.ConsulTLSPrivateKeyFile = writeTempPEM(t, "client.key", material.ClientKeyPEM)
	config.Config.ConsulTLSServerName = "consul.test"

	client := mustConsulClient(t)
	if err := putProbe(t, client); err != nil {
		t.Fatalf("mTLS: %v", err)
	}
}

func TestConsulClientMTLSMissingClientCert(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, true)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSCAFile = writeTempPEM(t, "ca.pem", material.CACertPEM)
	config.Config.ConsulTLSServerName = "consul.test"

	client := mustConsulClient(t)
	if err := putProbe(t, client); err == nil {
		t.Fatal("expected mTLS without client certificate to fail")
	}
}

func TestConsulClientUnpairedCertificateRejectedByFactory(t *testing.T) {
	_, err := newConsulClient(consulClientOptions{
		Address:     "https://127.0.0.1:8501",
		Scheme:      "https",
		TLSCertFile: writeTempPEM(t, "client.pem", []byte("not-a-cert")),
	})
	if err == nil {
		t.Fatal("expected unpaired client certificate to fail")
	}
}

func TestConsulClientMissingCAFileFailsAtInit(t *testing.T) {
	_, err := newConsulClient(consulClientOptions{
		Address:   "https://127.0.0.1:8501",
		Scheme:    "https",
		TLSCAFile: filepath.Join(t.TempDir(), "missing-ca.pem"),
	})
	if err == nil {
		t.Fatal("expected missing CA file to fail during client construction")
	}
}

func TestConsulClientSkipVerifyExplicit(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSSkipVerify = true

	client := mustConsulClient(t)
	if err := putProbe(t, client); err != nil {
		t.Fatalf("explicit skip verify: %v", err)
	}
}

func TestConsulEnvSSLVerifyCannotDisableVerification(t *testing.T) {
	t.Setenv("CONSUL_HTTP_SSL_VERIFY", "false")
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	configureConsulTest(t, server.URL, false)
	config.Config.ConsulTLSServerName = "consul.test"

	client := mustConsulClient(t)
	if err := putProbe(t, client); err == nil {
		t.Fatal("CONSUL_HTTP_SSL_VERIFY=false must not override the JSON secure default")
	}

	config.Config.ConsulTLSCAFile = writeTempPEM(t, "ca.pem", material.CACertPEM)
	secureClient := mustConsulClient(t)
	if err := putProbe(t, secureClient); err != nil {
		t.Fatalf("private CA should still work while env skip-verify is set: %v", err)
	}
}

func TestConsulClientHTTPTimeoutDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(true)
	}))
	t.Cleanup(server.Close)

	timedOut, err := newConsulClient(consulClientOptions{
		Address:            server.URL,
		Scheme:             "http",
		HTTPTimeoutSeconds: 1,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	err = putProbe(t, timedOut)
	if err == nil {
		t.Fatal("expected overall HTTP timeout")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "timeout") && !strings.Contains(strings.ToLower(err.Error()), "deadline") {
		t.Fatalf("error %q should mention timeout or deadline", err)
	}

	noDeadline, err := newConsulClient(consulClientOptions{
		Address:            server.URL,
		Scheme:             "http",
		HTTPTimeoutSeconds: 0,
	})
	if err != nil {
		t.Fatalf("no-deadline client: %v", err)
	}
	if err := putProbe(t, noDeadline); err != nil {
		t.Fatalf("explicit 0 timeout should wait for the handler: %v", err)
	}
}

func TestBothProvidersShareOfficialClientFactory(t *testing.T) {
	server := buildConsulTestServer(t, []consulTestServerOp{
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster",
			Request:  "v",
			Response: true,
		},
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)

	client := mustConsulClient(t)
	if err := NewConsulStore(client).PutKeyValue("mysql/master/cluster", "v"); err != nil {
		t.Fatalf("consul store: %v", err)
	}
	if err := NewConsulTxnStore(client).PutKeyValue("mysql/master/cluster", "v"); err != nil {
		t.Fatalf("consul-txn store: %v", err)
	}
}

func TestConsulClientUsesNormalizedHTTPSAddress(t *testing.T) {
	material := newTLSMaterial(t, "consul.test")
	server := startConsulTLSServer(t, kvPutOKHandler(t), material, false)
	host := strings.TrimPrefix(server.URL, "https://")
	client, err := newConsulClient(consulClientOptions{
		Address:       host,
		Scheme:        "https",
		TLSCAFile:     writeTempPEM(t, "ca.pem", material.CACertPEM),
		TLSServerName: "consul.test",
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := putProbe(t, client); err != nil {
		t.Fatalf("https host:port: %v", err)
	}
}

func TestConsulClientConstructionErrorIncludesContext(t *testing.T) {
	_, err := newConsulClient(consulClientOptions{
		Address: "ftp://127.0.0.1:8500",
	})
	if err == nil {
		t.Fatal("expected invalid scheme to fail")
	}
	if !strings.Contains(err.Error(), "create consul client") {
		t.Fatalf("error %q should wrap factory context", err)
	}
}

func TestSkipVerifyWarningDoesNotIncludeToken(t *testing.T) {
	if strings.Contains(strings.ToLower(consulTLSSkipVerifyWarning), "token") {
		t.Fatal("skip-verify warning must not mention token")
	}
}

func TestConsulTokenFileUsedWhenJSONEmpty(t *testing.T) {
	tokenPath := writeTempPEM(t, "consul.token", []byte("file-token\n"))
	t.Setenv("CONSUL_HTTP_TOKEN", "")
	t.Setenv("CONSUL_HTTP_TOKEN_FILE", tokenPath)

	var captured []capturedConsulRequest
	server := buildConsulTestServerWithObserver(t, []consulTestServerOp{
		{
			Method:   http.MethodPut,
			URL:      "/v1/kv/mysql/master/cluster",
			Request:  "v",
			Response: true,
		},
	}, func(req capturedConsulRequest) {
		captured = append(captured, req)
	})
	defer server.Close()
	configureConsulTest(t, server.URL, false)

	store := newTestConsulStore(t)
	if err := store.PutKeyValue("mysql/master/cluster", "v"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if len(captured) != 1 || captured[0].Token != "file-token" {
		t.Fatalf("expected file-token header, got %+v", captured)
	}
}
