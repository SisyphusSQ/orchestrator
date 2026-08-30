package orcraft

import (
	"errors"
	"testing"
)

func TestMonitorReturnsFatalRaftError(t *testing.T) {
	expectedErr := errors.New("raft transport failed")
	fatalErrors := make(chan error, 1)
	fatalErrors <- expectedErr

	err := monitor(nil, nil, nil, fatalErrors)
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
