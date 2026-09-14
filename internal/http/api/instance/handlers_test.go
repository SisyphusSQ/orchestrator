package instance

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/http/transport"
	"github.com/openark/orchestrator/internal/logic/discovery"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/repository"
)

type discoveryRaftFixture struct{}

func (discoveryRaftFixture) ApplyCommand(string, []byte) any    { return nil }
func (discoveryRaftFixture) GetData() ([]byte, error)           { return []byte("{}"), nil }
func (discoveryRaftFixture) Restore(reader io.ReadCloser) error { return reader.Close() }

func TestAsyncDiscoverRespondsOnceAndOutlivesRequest(t *testing.T) {
	// Only isolated SQLite, a single local Raft node and a loopback TCP fixture.
	raftPort, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	raftAddress := raftPort.Addr().String()
	raftPort.Close()
	target, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	previous := config.Current()
	config.TestUpdate(func(c *config.Configuration) {
		c.Metadata.Type = "sqlite3"
		c.Metadata.SQLite.DataFile = filepath.Join(t.TempDir(), "metadata.sqlite")
		c.Metadata.Schema.SkipUpdate = false
		c.Raft.NodeID = "async-discovery-test"
		c.Raft.DataDir = t.TempDir()
		c.Raft.Bind = raftAddress
		c.Raft.Advertise = raftAddress
		c.Server.HTTPAdvertise = "http://127.0.0.1:3000"
		c.Server.TLS.Enabled = false
		c.Server.ReadOnly = false
		c.Authentication.Method = ""
		c.Topology.Hostname.ResolveMethod = "none"
		c.MySQL.ConnectTimeoutSeconds = 30
		c.Topology.MySQL.DiscoveryReadTimeoutSeconds = 30
	})
	t.Cleanup(func() {
		_ = orcraft.Shutdown()
		_ = repository.Close()
		config.TestUpdate(func(c *config.Configuration) { *c = *previous })
	})
	if err := repository.InitializeMetadata(t.Context()); err != nil {
		t.Fatal(err)
	}
	app := discoveryRaftFixture{}
	if err := orcraft.Setup(app, app, "localhost"); err != nil {
		t.Fatal(err)
	}
	if _, err := orcraft.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !orcraft.IsLeaderReady() {
		if time.Now().After(deadline) {
			t.Fatal("local Raft leader not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	m := discovery.NewManual(t.Context())
	defer m.Close()
	api := API{Manual: m}
	router, err := transport.NewRouter(transport.RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	router.Get("/api/async-discover/:host/:port", api.AsyncDiscover)
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, err := target.Accept()
		if err == nil {
			accepted <- connection
		}
	}()
	requestCtx, cancelRequest := context.WithCancel(t.Context())
	defer cancelRequest()
	port := target.Addr().(*net.TCPAddr).Port
	request := httptest.NewRequest(http.MethodGet, "/api/async-discover/127.0.0.1/"+strconv.Itoa(port), nil).WithContext(requestCtx)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	// ResponseCode is encoded as a string, so decode its wire form here.
	var wire struct{ Code string }
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Code != "OK" {
		t.Fatalf("submission rejected: %s", response.Body.String())
	}
	original := response.Body.String()
	var connection net.Conn
	select {
	case connection = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("background discovery did not connect")
	}
	defer connection.Close()
	cancelRequest()
	// The TCP fixture does not send a MySQL greeting. A disconnected request
	// must leave this accepted task waiting, until the service cancels it.
	connection.SetReadDeadline(time.Now().Add(1100 * time.Millisecond))
	var b [1]byte
	_, err = connection.Read(b[:])
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("request cancellation stopped background task: %v", err)
	}
	m.Close()
	if response.Body.String() != original {
		t.Fatal("background task wrote another HTTP response")
	}
}
