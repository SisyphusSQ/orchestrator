package orcraft

import (
	"errors"
	"testing"

	"github.com/openark/orchestrator/go/config"
)

func TestMonitorReturnsFatalRaftError(t *testing.T) {
	expectedErr := errors.New("raft transport failed")
	fatalErrors := make(chan error, 1)
	fatalErrors <- expectedErr

	err := monitor(nil, nil, fatalErrors)
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
