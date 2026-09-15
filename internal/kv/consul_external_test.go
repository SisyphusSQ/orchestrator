package kv

import (
	"fmt"
	"os"
	"strings"
	"testing"

	consulapi "github.com/hashicorp/consul/api"

	"github.com/openark/orchestrator/internal/config"
)

func TestConsulExternalThreeServerLifecycle(t *testing.T) {
	addressesValue := os.Getenv("ORCH_CONSUL_E2E_ADDRESSES")
	if addressesValue == "" {
		t.Skip("set ORCH_CONSUL_E2E_ADDRESSES to three comma-separated Consul HTTP addresses")
	}
	addresses := strings.Split(addressesValue, ",")
	if len(addresses) != 3 {
		t.Fatalf("ORCH_CONSUL_E2E_ADDRESSES has %d addresses; want exactly three", len(addresses))
	}

	scheme := envOrDefault("ORCH_CONSUL_E2E_SCHEME", "http")
	datacenter := envOrDefault("ORCH_CONSUL_E2E_DATACENTER", "too415-dc1")
	token := readOptionalSecretFile(t, "ORCH_CONSUL_E2E_TOKEN_FILE")
	baseOptions := consulClientOptions{
		Scheme:             scheme,
		Token:              token,
		Datacenter:         datacenter,
		TLSCAFile:          os.Getenv("ORCH_CONSUL_E2E_CA_FILE"),
		TLSCertFile:        os.Getenv("ORCH_CONSUL_E2E_CERT_FILE"),
		TLSPrivateKeyFile:  os.Getenv("ORCH_CONSUL_E2E_KEY_FILE"),
		TLSServerName:      os.Getenv("ORCH_CONSUL_E2E_SERVER_NAME"),
		HTTPTimeoutSeconds: 5,
	}

	clients := make([]*consulapi.Client, 0, len(addresses))
	for _, address := range addresses {
		options := baseOptions
		options.Address = strings.TrimSpace(address)
		client, err := newConsulClient(options)
		if err != nil {
			t.Fatalf("create Consul client for %s: %v", address, err)
		}
		clients = append(clients, client)
	}

	configuration, err := clients[0].Operator().RaftGetConfiguration(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(configuration.Servers) != 3 {
		t.Fatalf("Consul Raft server count = %d, want 3", len(configuration.Servers))
	}
	leaders := 0
	for _, server := range configuration.Servers {
		if server.Leader {
			leaders++
		}
		if !server.Voter {
			t.Fatalf("Consul server is not a voter: %+v", server)
		}
	}
	if leaders != 1 {
		t.Fatalf("Consul leaders = %d, want 1", leaders)
	}

	prefix := "too415/real-three-server"
	txnStore := NewConsulTxnStore(clients[0])
	if err := txnStore.PutKVPairs([]*KVPair{
		{Key: prefix + "/writer", Value: "mysql.example:3306"},
		{Key: prefix + "/generation", Value: "415"},
	}); err != nil {
		t.Fatal(err)
	}
	plainStore := NewConsulStore(clients[1])
	if err := plainStore.PutKeyValue(prefix+"/plain", "replicated"); err != nil {
		t.Fatal(err)
	}

	for index, client := range clients {
		for key, want := range map[string]string{
			prefix + "/writer":     "mysql.example:3306",
			prefix + "/generation": "415",
			prefix + "/plain":      "replicated",
		} {
			pair, _, err := client.KV().Get(key, nil)
			if err != nil {
				t.Fatalf("node %d read %s: %v", index+1, key, err)
			}
			if pair == nil || string(pair.Value) != want {
				t.Fatalf("node %d read %s = %v, want %q", index+1, key, pair, want)
			}
		}
	}

	if scheme == "https" {
		testConsulExternalSecurity(t, baseOptions, strings.TrimSpace(addresses[0]))
	}

	t.Log(fmt.Sprintf("real Consul cluster passed: %d voting servers, one leader, transactional and plain KV read from all three agents over %s", len(configuration.Servers), scheme))
}

func testConsulExternalSecurity(t *testing.T, baseOptions consulClientOptions, address string) {
	t.Helper()
	if baseOptions.Token == "" || baseOptions.TLSCAFile == "" || baseOptions.TLSCertFile == "" || baseOptions.TLSPrivateKeyFile == "" {
		t.Fatal("secure Consul E2E requires token, CA, client certificate, and client key files")
	}

	withoutToken := baseOptions
	withoutToken.Address = address
	withoutToken.Token = ""
	anonymousClient, err := newConsulClient(withoutToken)
	if err != nil {
		t.Fatalf("create anonymous mTLS client: %v", err)
	}
	if _, err := anonymousClient.KV().Put(&consulapi.KVPair{Key: "too415/secure/anonymous-denied", Value: []byte("must-not-write")}, nil); err == nil {
		t.Fatal("anonymous Consul KV write unexpectedly passed with ACL default deny")
	}

	withoutClientCertificate := baseOptions
	withoutClientCertificate.Address = address
	withoutClientCertificate.TLSCertFile = ""
	withoutClientCertificate.TLSPrivateKeyFile = ""
	noCertificateClient, err := newConsulClient(withoutClientCertificate)
	if err != nil {
		t.Fatalf("create CA-only Consul client: %v", err)
	}
	if _, err := noCertificateClient.Status().Leader(); err == nil {
		t.Fatal("Consul HTTPS request without a client certificate unexpectedly passed")
	}

	managementOptions := baseOptions
	managementOptions.Address = address
	managementClient, err := newConsulClient(managementOptions)
	if err != nil {
		t.Fatalf("create management Consul client: %v", err)
	}
	policy, _, err := managementClient.ACL().PolicyCreate(&consulapi.ACLPolicy{
		Name:  "too415-prefix-writer",
		Rules: `key_prefix "too415/secure/allowed/" { policy = "write" }`,
	}, nil)
	if err != nil {
		t.Fatalf("create prefix ACL policy: %v", err)
	}
	t.Cleanup(func() {
		if _, err := managementClient.ACL().PolicyDelete(policy.ID, nil); err != nil {
			t.Errorf("delete prefix ACL policy: %v", err)
		}
	})
	token, _, err := managementClient.ACL().TokenCreate(&consulapi.ACLToken{
		Description: "TOO-415 temporary prefix writer",
		Policies:    []*consulapi.ACLTokenPolicyLink{{ID: policy.ID}},
	}, nil)
	if err != nil {
		t.Fatalf("create prefix ACL token: %v", err)
	}
	t.Cleanup(func() {
		if _, err := managementClient.ACL().TokenDelete(token.AccessorID, nil); err != nil {
			t.Errorf("delete prefix ACL token: %v", err)
		}
	})

	restrictedOptions := baseOptions
	restrictedOptions.Address = address
	restrictedOptions.Token = token.SecretID
	restrictedClient, err := newConsulClient(restrictedOptions)
	if err != nil {
		t.Fatalf("create restricted Consul client: %v", err)
	}
	allowedKey := "too415/secure/allowed/writer"
	if _, err := restrictedClient.KV().Put(&consulapi.KVPair{Key: allowedKey, Value: []byte("mysql.example:3306")}, nil); err != nil {
		t.Fatalf("write allowed prefix with restricted ACL token: %v", err)
	}
	pair, _, err := restrictedClient.KV().Get(allowedKey, nil)
	if err != nil || pair == nil || string(pair.Value) != "mysql.example:3306" {
		t.Fatalf("read allowed prefix with restricted ACL token: pair=%v err=%v", pair, err)
	}
	if _, err := restrictedClient.KV().Put(&consulapi.KVPair{Key: "too415/secure/denied/writer", Value: []byte("must-not-write")}, nil); err == nil {
		t.Fatal("write outside restricted ACL prefix unexpectedly passed")
	}
}

func envOrDefault(name string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func readOptionalSecretFile(t *testing.T, environmentName string) string {
	t.Helper()
	filename := strings.TrimSpace(os.Getenv(environmentName))
	if filename == "" {
		return ""
	}
	contents, err := os.ReadFile(filename)
	if err != nil {
		t.Fatalf("read secret file from %s: %v", environmentName, err)
	}
	secret := strings.TrimSpace(string(contents))
	if secret == "" {
		t.Fatalf("secret file from %s is empty", environmentName)
	}
	return secret
}

func TestConsulExternalCrossDatacenterDistribution(t *testing.T) {
	clusterValue := strings.TrimSpace(os.Getenv("ORCH_CONSUL_E2E_CROSS_DC_ADDRESSES"))
	if clusterValue == "" {
		t.Skip("set ORCH_CONSUL_E2E_CROSS_DC_ADDRESSES to dc=address|address|address entries")
	}

	dcAddresses := make(map[string][]string)
	for _, entry := range strings.Split(clusterValue, ",") {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			t.Fatalf("invalid cross-DC entry %q", entry)
		}
		datacenter := strings.TrimSpace(parts[0])
		addresses := strings.Split(parts[1], "|")
		if len(addresses) != 3 {
			t.Fatalf("datacenter %s has %d addresses; want exactly three", datacenter, len(addresses))
		}
		dcAddresses[datacenter] = addresses
	}
	if len(dcAddresses) != 2 {
		t.Fatalf("cross-DC fixture has %d datacenters; want exactly two", len(dcAddresses))
	}

	var sourceDatacenter string
	for datacenter := range dcAddresses {
		if sourceDatacenter == "" || datacenter < sourceDatacenter {
			sourceDatacenter = datacenter
		}
	}
	sourceClient, err := newConsulClient(consulClientOptions{
		Address:            strings.TrimSpace(dcAddresses[sourceDatacenter][0]),
		Scheme:             "http",
		Datacenter:         sourceDatacenter,
		HTTPTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("create source Consul client: %v", err)
	}
	datacenters, err := sourceClient.Catalog().Datacenters()
	if err != nil {
		t.Fatalf("list Consul datacenters: %v", err)
	}
	if len(datacenters) != 2 {
		t.Fatalf("Consul catalog datacenters = %v, want two", datacenters)
	}

	previousDistribution := config.Current().Consul.KV.CrossDataCenterDistribution
	config.TestUpdate(func(configuration *config.Configuration) {
		configuration.Consul.KV.CrossDataCenterDistribution = true
	})
	t.Cleanup(func() {
		config.TestUpdate(func(configuration *config.Configuration) {
			configuration.Consul.KV.CrossDataCenterDistribution = previousDistribution
		})
	})
	prefix := "too415/cross-dc"
	store := NewConsulStore(sourceClient)
	if err := store.DistributePairs([]*KVPair{
		{Key: prefix + "/writer", Value: "mysql.example:3306"},
		{Key: prefix + "/generation", Value: "415"},
	}); err != nil {
		t.Fatalf("distribute Consul pairs across datacenters: %v", err)
	}

	for datacenter, addresses := range dcAddresses {
		for index, address := range addresses {
			client, err := newConsulClient(consulClientOptions{
				Address:            strings.TrimSpace(address),
				Scheme:             "http",
				Datacenter:         datacenter,
				HTTPTimeoutSeconds: 5,
			})
			if err != nil {
				t.Fatalf("create client for %s node %d: %v", datacenter, index+1, err)
			}
			for key, want := range map[string]string{
				prefix + "/writer":     "mysql.example:3306",
				prefix + "/generation": "415",
			} {
				pair, _, err := client.KV().Get(key, &consulapi.QueryOptions{Datacenter: datacenter, RequireConsistent: true})
				if err != nil {
					t.Fatalf("read %s from %s node %d: %v", key, datacenter, index+1, err)
				}
				if pair == nil || string(pair.Value) != want {
					t.Fatalf("read %s from %s node %d = %v, want %q", key, datacenter, index+1, pair, want)
				}
			}
		}
	}
	t.Log("real Consul cross-datacenter distribution passed: two datacenters, three servers each, all six agents read both distributed keys")
}
