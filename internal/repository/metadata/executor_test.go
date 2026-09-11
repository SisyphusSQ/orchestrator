package metadata

import (
	"context"
	"errors"
	"testing"
)

func TestBackendWriteObservationPreservesResultAndReleasesSlot(t *testing.T) {
	previous := writeSlots
	writeSlots = make(chan struct{}, 1)
	t.Cleanup(func() { writeSlots = previous })
	expected := errors.New("write failed")
	if err := ExecuteWrite(t.Context(), func() error { return expected }); !errors.Is(err, expected) {
		t.Fatalf("lost error: %v", err)
	}
	if len(writeSlots) != 0 {
		t.Fatal("leaked semaphore slot")
	}
	// Existing non-runtime panic handling is intentionally preserved by this migration.
	if err := ExecuteWrite(t.Context(), func() error { panic("existing panic contract") }); err != nil {
		t.Fatal(err)
	}
	if len(writeSlots) != 0 {
		t.Fatal("panic leaked semaphore slot")
	}
	writeSlots <- struct{}{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	called := false
	if err := ExecuteWrite(ctx, func() error { called = true; return nil }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if called {
		t.Fatal("canceled queued task ran")
	}
	<-writeSlots
}
