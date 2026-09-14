package config

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
)

var configurationMu sync.Mutex
var activeConfiguration atomic.Pointer[Configuration]

func init() { activeConfiguration.Store(newConfiguration()) }

// Current returns the published, immutable configuration. Callers must retain
// this snapshot for an operation and must never mutate it or its maps/slices.
func Current() *Configuration { return activeConfiguration.Load() }

type snapshotContextKey struct{}

// WithSnapshot pins configuration for the lifetime of a logical operation.
func WithSnapshot(ctx context.Context) context.Context {
	if _, ok := ctx.Value(snapshotContextKey{}).(*Configuration); ok {
		return ctx
	}
	return context.WithValue(ctx, snapshotContextKey{}, Current())
}

func FromContext(ctx context.Context) *Configuration {
	if cfg, ok := ctx.Value(snapshotContextKey{}).(*Configuration); ok {
		return cfg
	}
	return Current()
}

// TestUpdate publishes a copy for test fixtures without production validation.
// Production code must use Read/ForceRead/Reload and never this test seam.
func TestUpdate(update func(*Configuration)) {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	candidate, err := cloneConfiguration(Current())
	if err != nil {
		panic(err)
	}
	update(candidate)
	candidate, err = cloneConfiguration(candidate)
	if err != nil {
		panic(err)
	}
	activeConfiguration.Store(candidate)
}

// EnableDatabaseUpdate applies the explicit CLI override before services start.
func EnableDatabaseUpdate() error {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	if telemetryConfigurationLocked {
		return fmt.Errorf("database update override requires startup")
	}
	candidate, err := cloneConfiguration(Current())
	if err != nil {
		return err
	}
	candidate.Metadata.Schema.SkipUpdate = false
	activeConfiguration.Store(candidate)
	return nil
}

// restartProjection removes only fields whose consumers read them dynamically.
// Everything else is startup configuration, including future fields by default.
func restartProjection(cfg *Configuration) Configuration {
	p := *cfg
	p.Server.ReadOnly = false
	p.Server.Web = WebConfiguration{}
	p.OSC = OSCConfiguration{}
	return p
}

func validateReload(current, candidate *Configuration) error {
	if telemetryConfigurationLocked && !reflect.DeepEqual(restartProjection(current), restartProjection(candidate)) {
		return fmt.Errorf("configuration changes outside server.readOnly, server.web and osc require a process restart")
	}
	return nil
}

// PrepareRaft normalizes and validates the server runtime before startup without
// mutating a snapshot already retained by another consumer.
func PrepareRaft() error {
	configurationMu.Lock()
	defer configurationMu.Unlock()
	candidate, err := cloneConfiguration(Current())
	if err != nil {
		return err
	}
	if err := candidate.normalizeAndValidateRaft(); err != nil {
		return err
	}
	activeConfiguration.Store(candidate)
	return nil
}
