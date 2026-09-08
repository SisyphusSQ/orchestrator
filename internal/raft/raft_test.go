package orcraft

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/openark/orchestrator/internal/config"
)

func TestRuntimeShutdownWhileServingStatusAndCommands(t *testing.T) {
	previous := config.Config
	cfg := *previous
	cfg.RaftNodeID = "runtime-test"
	cfg.RaftDataDir = t.TempDir()
	cfg.RaftBind = localAddr(t)
	cfg.RaftAdvertise = cfg.RaftBind
	cfg.HTTPAdvertise = "http://127.0.0.1:3000"
	cfg.UseSSL = false
	config.Config = &cfg
	t.Cleanup(func() { _ = Shutdown(); config.Config = previous })
	app := &memoryApp{}
	if err := Setup(app, app, "localhost"); err != nil {
		t.Fatal(err)
	}
	if _, err := PublishCommand("set", "before-bootstrap"); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("unbootstrapped command: %v", err)
	}
	if _, err := Bootstrap(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 5*time.Second, "single voter leader", IsLeaderReady)
	var readers sync.WaitGroup
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	for range 3 {
		readers.Go(func() {
			for ctx.Err() == nil {
				GetStatus()
				LogProgress()
				_, _ = PublishCommand("set", "during-shutdown")
			}
		})
	}
	if err := Shutdown(); err != nil {
		t.Fatal(err)
	}
	cancel()
	readers.Wait()
	if IsInitialized() || IsReady() || IsLeaderReady() {
		t.Fatal("closed runtime is ready")
	}
	if _, err := PublishCommand("set", "after-shutdown"); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("closed runtime command: %v", err)
	}
}

func TestMonitorStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := monitor(ctx, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestMonitorReturnsFatalRaftError(t *testing.T) {
	expectedErr := errors.New("raft transport failed")
	fatalErrors := make(chan error, 1)
	fatalErrors <- expectedErr

	err := monitor(t.Context(), nil, nil, fatalErrors)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("monitor() error = %v; want wrapped %v", err, expectedErr)
	}
}

func TestEnqueueFatalRaftErrorKeepsFirstErrorWithoutBlocking(t *testing.T) {
	fatalErrors := make(chan error, 1)
	firstErr := errors.New("first failure")
	secondErr := errors.New("second failure")

	if !enqueueFatalRaftError(fatalErrors, firstErr) {
		t.Fatal("enqueueFatalRaftError() rejected the first error")
	}
	if enqueueFatalRaftError(fatalErrors, secondErr) {
		t.Fatal("enqueueFatalRaftError() accepted a second error while one was pending")
	}
	if got := <-fatalErrors; !errors.Is(got, firstErr) {
		t.Fatalf("queued error = %v; want %v", got, firstErr)
	}
}

func TestComputeLeaderURIHandlesIPv6(t *testing.T) {
	originalConfig := config.Config
	testConfig := *config.Config
	config.Config = &testConfig
	t.Cleanup(func() { config.Config = originalConfig })

	config.Config.HTTPAdvertise = ""
	config.Config.UseSSL = false
	config.Config.RaftAdvertise = "[::1]:10008"
	config.Config.ListenAddress = "[::]:3000"

	got, err := computeLeaderURI()
	if err != nil {
		t.Fatalf("computeLeaderURI: %v", err)
	}
	if want := "http://[::1]:3000"; got != want {
		t.Fatalf("computeLeaderURI = %q, want %q", got, want)
	}

	config.Config.ListenAddress = "3000"
	if _, err := computeLeaderURI(); err == nil {
		t.Fatal("computeLeaderURI accepted a listen address without host:port syntax")
	}
}
