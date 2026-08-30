package inst

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/openark/orchestrator/go/config"
)

func TestAuditOperationReturnsFileWriteFailure(t *testing.T) {
	previousAuditLogFile := config.Config.AuditLogFile
	previousAuditToBackendDB := config.Config.AuditToBackendDB
	previousSyslogWriter := syslogWriter
	config.Config.AuditLogFile = filepath.Join(t.TempDir(), "missing", "audit.log")
	config.Config.AuditToBackendDB = false
	syslogWriter = nil
	t.Cleanup(func() {
		config.Config.AuditLogFile = previousAuditLogFile
		config.Config.AuditToBackendDB = previousAuditToBackendDB
		syslogWriter = previousSyslogWriter
	})

	if err := AuditOperation("test", nil, "operation completed"); err == nil {
		t.Fatal("AuditOperation() returned nil for an unavailable audit log path")
	}
}

func TestAuditOperationWaitsForSyslogWrite(t *testing.T) {
	previousAuditLogFile := config.Config.AuditLogFile
	previousAuditToBackendDB := config.Config.AuditToBackendDB
	sink := newBlockingAuditSyslogSink()
	previousSyslogWriter := swapAuditSyslogWriter(sink)
	config.Config.AuditLogFile = ""
	config.Config.AuditToBackendDB = false
	t.Cleanup(func() {
		config.Config.AuditLogFile = previousAuditLogFile
		config.Config.AuditToBackendDB = previousAuditToBackendDB
		swapAuditSyslogWriter(previousSyslogWriter)
	})

	completed := make(chan error, 1)
	go func() {
		completed <- AuditOperation("test", nil, "operation completed")
	}()

	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("audit syslog write did not start")
	}
	select {
	case err := <-completed:
		t.Fatalf("AuditOperation() returned before syslog completed: %v", err)
	default:
	}

	close(sink.release)
	select {
	case err := <-completed:
		if err != nil {
			t.Fatalf("AuditOperation() error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("AuditOperation() did not return after syslog completed")
	}
}

func TestAuditOperationReturnsSyslogWriteFailure(t *testing.T) {
	previousAuditLogFile := config.Config.AuditLogFile
	previousAuditToBackendDB := config.Config.AuditToBackendDB
	expectedErr := errors.New("syslog unavailable")
	previousSyslogWriter := swapAuditSyslogWriter(errorAuditSyslogSink{err: expectedErr})
	config.Config.AuditLogFile = ""
	config.Config.AuditToBackendDB = false
	t.Cleanup(func() {
		config.Config.AuditLogFile = previousAuditLogFile
		config.Config.AuditToBackendDB = previousAuditToBackendDB
		swapAuditSyslogWriter(previousSyslogWriter)
	})

	err := AuditOperation("test", nil, "operation completed")
	if !errors.Is(err, expectedErr) {
		t.Fatalf("AuditOperation() error = %v; want wrapped %v", err, expectedErr)
	}
}

func TestCloseAuditSyslogClosesAndDisablesSink(t *testing.T) {
	sink := &closeTrackingAuditSyslogSink{}
	previousSyslogWriter := swapAuditSyslogWriter(sink)
	t.Cleanup(func() {
		swapAuditSyslogWriter(previousSyslogWriter)
	})

	if err := CloseAuditSyslog(); err != nil {
		t.Fatalf("CloseAuditSyslog() error: %v", err)
	}
	if !sink.closed {
		t.Fatal("CloseAuditSyslog() did not close the sink")
	}
	syslogMutex.RLock()
	defer syslogMutex.RUnlock()
	if syslogWriter != nil {
		t.Fatal("CloseAuditSyslog() left the sink enabled")
	}
}

func swapAuditSyslogWriter(writer auditSyslogSink) auditSyslogSink {
	syslogMutex.Lock()
	previous := syslogWriter
	syslogWriter = writer
	syslogMutex.Unlock()
	return previous
}

type blockingAuditSyslogSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingAuditSyslogSink() *blockingAuditSyslogSink {
	return &blockingAuditSyslogSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (sink *blockingAuditSyslogSink) Info(string) error {
	sink.once.Do(func() {
		close(sink.started)
	})
	<-sink.release
	return nil
}

func (*blockingAuditSyslogSink) Close() error { return nil }

type errorAuditSyslogSink struct {
	err error
}

func (sink errorAuditSyslogSink) Info(string) error { return sink.err }
func (errorAuditSyslogSink) Close() error           { return nil }

type closeTrackingAuditSyslogSink struct {
	closed bool
}

func (*closeTrackingAuditSyslogSink) Info(string) error { return nil }
func (sink *closeTrackingAuditSyslogSink) Close() error {
	sink.closed = true
	return nil
}
