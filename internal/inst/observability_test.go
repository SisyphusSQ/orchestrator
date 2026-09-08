package inst

import (
	"context"
	"errors"
	"testing"
)

func TestBackendWriteObservationPreservesResultAndReleasesSlot(t *testing.T) {
	previous := instanceWriteChan
	instanceWriteChan = make(chan bool, 1)
	t.Cleanup(func() { instanceWriteChan = previous })
	expected := errors.New("write failed")
	if err := ExecDBWriteFunc(func() error { return expected }); !errors.Is(err, expected) {
		t.Fatalf("lost error: %v", err)
	}
	if len(instanceWriteChan) != 0 {
		t.Fatal("leaked semaphore slot")
	}
	// Existing non-runtime panic handling is intentionally preserved by this migration.
	if err := ExecDBWriteFunc(func() error { panic("existing panic contract") }); err != nil {
		t.Fatal(err)
	}
	if len(instanceWriteChan) != 0 {
		t.Fatal("panic leaked semaphore slot")
	}
	instanceWriteChan <- true
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	if err := ExecDBWriteFuncContext(ctx, func() error { called = true; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if called {
		t.Fatal("canceled queued task ran")
	}
	<-instanceWriteChan
}
