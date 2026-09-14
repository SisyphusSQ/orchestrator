package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	instmodel "github.com/openark/orchestrator/internal/inst/instance"
)

func TestManualDiscoveryOwnsLifetimeAndBoundsConcurrency(t *testing.T) {
	serviceCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewManual(serviceCtx)
	m.slots = make(chan struct{}, 1)
	entered := make(chan context.Context, 1)
	recorded := make(chan error, 1)
	m.discover = func(ctx context.Context, _ *instmodel.InstanceKey) (*instmodel.Instance, error) {
		entered <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}
	m.record = func(_ *instmodel.InstanceKey, err error) { recorded <- err }
	if err := m.Submit(instmodel.InstanceKey{Hostname: "test", Port: 3306}); err != nil {
		t.Fatal(err)
	}
	ctx := <-entered
	if ctx.Err() != nil {
		t.Fatal("task prematurely canceled")
	}
	if err := m.Submit(instmodel.InstanceKey{}); err == nil {
		t.Fatal("capacity limit ignored")
	}
	m.Close()
	if err := <-recorded; !errors.Is(err, context.Canceled) {
		t.Fatalf("outcome=%v", err)
	}
	if err := m.Submit(instmodel.InstanceKey{}); err == nil {
		t.Fatal("accepted work after shutdown")
	}
}
func TestManualDiscoveryTimeoutIsRecorded(t *testing.T) {
	m := NewManual(context.Background())
	defer m.Close()
	m.timeout = 10 * time.Millisecond
	recorded := make(chan error, 1)
	m.discover = func(ctx context.Context, _ *instmodel.InstanceKey) (*instmodel.Instance, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	m.record = func(_ *instmodel.InstanceKey, err error) { recorded <- err }
	if err := m.Submit(instmodel.InstanceKey{Hostname: "test", Port: 3306}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-recorded:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout did not stop discovery")
	}
}
