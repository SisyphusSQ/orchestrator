package log

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestConsoleEncoderMatchesApprovedFormat(t *testing.T) {
	entry := zapcore.Entry{
		Time:    time.Date(2026, time.August, 30, 21, 36, 12, 0, time.Local),
		Level:   zapcore.InfoLevel,
		Message: "operation completed",
		Caller: zapcore.EntryCaller{
			Defined: true,
			File:    "/repo/go/logic/orchestrator.go",
			Line:    123,
		},
	}
	buffer, err := newConsoleEncoder().EncodeEntry(entry, []zapcore.Field{
		zap.String("component", "logic"),
	})
	if err != nil {
		t.Fatalf("EncodeEntry() error: %v", err)
	}
	defer buffer.Free()

	want := "2026-08-30 21:36:12\t[INFO]\t[logic/orchestrator.go:123]\toperation completed\t{\"component\": \"logic\"}\n"
	if got := buffer.String(); got != want {
		t.Fatalf("encoded entry = %q; want %q", got, want)
	}
}

func TestInfofUsesApprovedConsoleFormat(t *testing.T) {
	previousLevel := GetLevel()
	SetLevel(INFO)
	t.Cleanup(func() {
		SetLevel(previousLevel)
	})

	output := captureStderr(t, func() {
		Infof("operation completed: %d", 7)
	})

	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\t\[INFO\]\t\[log/log_test\.go:\d+\]\toperation completed: 7\n$`)
	if !pattern.MatchString(output) {
		t.Fatalf("unexpected log output: %q", output)
	}
}

func TestInfoUsesPublicCaller(t *testing.T) {
	previousLevel := GetLevel()
	SetLevel(INFO)
	t.Cleanup(func() {
		SetLevel(previousLevel)
	})

	output := captureStderr(t, func() {
		Info("operation completed")
	})

	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\t\[INFO\]\t\[log/log_test\.go:\d+\]\toperation completed\n$`)
	if !pattern.MatchString(output) {
		t.Fatalf("unexpected log output: %q", output)
	}
}

func TestApplicationLogUsesStderrWithoutPollutingStdout(t *testing.T) {
	previousLevel := GetLevel()
	SetLevel(INFO)
	t.Cleanup(func() {
		SetLevel(previousLevel)
	})

	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() {
			Infof("operation completed")
		})
	})
	if stdout != "" {
		t.Fatalf("stdout = %q; want empty output", stdout)
	}
	if !strings.Contains(stderr, "operation completed") {
		t.Fatalf("stderr does not contain the application log: %q", stderr)
	}
}

func TestErroreReturnsOriginalErrorAndUsesPublicCaller(t *testing.T) {
	previousLevel := GetLevel()
	SetLevel(ERROR)
	t.Cleanup(func() {
		SetLevel(previousLevel)
	})

	expectedErr := errors.New("read failed")
	var actualErr error
	output := captureStderr(t, func() {
		actualErr = Errore(expectedErr)
	})

	if actualErr != expectedErr {
		t.Fatalf("Errore() returned %v; want original error %v", actualErr, expectedErr)
	}
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\t\[ERROR\]\t\[log/log_test\.go:\d+\]\tread failed\n$`)
	if !pattern.MatchString(output) {
		t.Fatalf("unexpected log output: %q", output)
	}
}

func TestErroreNilDoesNotLog(t *testing.T) {
	output := captureStderr(t, func() {
		if err := Errore(nil); err != nil {
			t.Fatalf("Errore(nil) = %v; want nil", err)
		}
	})
	if output != "" {
		t.Fatalf("Errore(nil) output = %q; want empty output", output)
	}
}

func TestErrorfReturnsFormattedError(t *testing.T) {
	previousLevel := GetLevel()
	SetLevel(ERROR)
	t.Cleanup(func() {
		SetLevel(previousLevel)
	})

	var actualErr error
	captureStderr(t, func() {
		actualErr = Errorf("read %s failed with status %d", "topology", 503)
	})
	if actualErr == nil {
		t.Fatal("Errorf() returned nil")
	}
	if !strings.Contains(actualErr.Error(), "read topology failed with status 503") {
		t.Fatalf("Errorf() = %q; want formatted message", actualErr)
	}
}

func TestStackTraceCanBeEnabled(t *testing.T) {
	previous := printStackTrace.Load()
	SetPrintStackTrace(true)
	t.Cleanup(func() {
		SetPrintStackTrace(previous)
	})

	output := captureStderr(t, func() {
		Errore(errors.New("read failed"))
	})
	if !strings.Contains(output, "runtime/debug.Stack") {
		t.Fatalf("stacktrace output is missing runtime/debug.Stack: %q", output)
	}
}

func TestConcurrentSetPrintStackTraceAndErrorLogging(t *testing.T) {
	previous := printStackTrace.Load()
	previousLevel := GetLevel()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	previousStderr := os.Stderr
	os.Stderr = devNull
	SetLevel(ERROR)
	t.Cleanup(func() {
		os.Stderr = previousStderr
		SetPrintStackTrace(previous)
		SetLevel(previousLevel)
		if err := devNull.Close(); err != nil {
			t.Errorf("close %s: %v", os.DevNull, err)
		}
	})

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 1_000; index++ {
			SetPrintStackTrace(index%2 == 0)
		}
	}()
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 1_000; index++ {
			Errore(errors.New("read failed"))
		}
	}()
	waitGroup.Wait()
}

func TestFatalFlushesClosesAndExits(t *testing.T) {
	markerPath := filepath.Join(t.TempDir(), "syslog-closed")
	hookMarkerPath := filepath.Join(t.TempDir(), "hook-closed")
	command := exec.Command(os.Args[0], "-test.run=^TestFatalHelperProcess$")
	command.Env = append(os.Environ(),
		"GO_WANT_FATAL_HELPER=1",
		"FATAL_CLOSE_MARKER="+markerPath,
		"FATAL_HOOK_CLOSE_MARKER="+hookMarkerPath,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("fatal helper error = %v; want process exit error", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("fatal helper exit code = %d; want 1", exitErr.ExitCode())
	}
	if stdout.Len() != 0 {
		t.Fatalf("fatal helper stdout = %q; want empty output", stdout.String())
	}
	pattern := regexp.MustCompile(`\t\[FATAL\]\t\[log/log_test\.go:\d+\]\tfatal operation: 7\n`)
	if !pattern.MatchString(stderr.String()) {
		t.Fatalf("fatal helper stderr does not contain approved fatal output: %q", stderr.String())
	}
	if _, err := os.Stat(markerPath); err != nil {
		t.Fatalf("syslog close marker is missing: %v", err)
	}
	if _, err := os.Stat(hookMarkerPath); err != nil {
		t.Fatalf("close hook marker is missing: %v", err)
	}
}

func TestFatalHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_FATAL_HELPER") != "1" {
		return
	}
	SetLevel(DEBUG)
	SetSyslogLevel(DEBUG)
	syslogWriter = markerSyslogSink{path: os.Getenv("FATAL_CLOSE_MARKER")}
	RegisterCloseHook(func() error {
		return os.WriteFile(os.Getenv("FATAL_HOOK_CLOSE_MARKER"), []byte("closed\n"), 0600)
	})
	Fatalf("fatal operation: %d", 7)
}

func TestSugarAppendsStructuredFieldsAfterMessage(t *testing.T) {
	previousLevel := GetLevel()
	SetLevel(INFO)
	t.Cleanup(func() {
		SetLevel(previousLevel)
	})

	output := captureStderr(t, func() {
		Sugar().Infow("operation completed", "component", "discovery", "port", 3306)
	})

	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\t\[INFO\]\t\[log/log_test\.go:\d+\]\toperation completed\t\{"component": "discovery", "port": 3306\}\n$`)
	if !pattern.MatchString(output) {
		t.Fatalf("unexpected structured log output: %q", output)
	}
}

func TestSugarDistributesStructuredEntryToSyslog(t *testing.T) {
	previousLevel := GetLevel()
	previousSyslogLevel := LogLevel(syslogLevel.Load())
	previousSyslogWriter := syslogWriter
	sink := &recordingSyslogSink{}
	SetLevel(INFO)
	SetSyslogLevel(INFO)
	syslogWriter = sink
	t.Cleanup(func() {
		syslogWriter = previousSyslogWriter
		SetSyslogLevel(previousSyslogLevel)
		SetLevel(previousLevel)
	})

	Sugar().Infow("operation completed", "component", "discovery", "port", 3306)

	if sink.priority != "info" {
		t.Fatalf("priority = %q; want %q", sink.priority, "info")
	}
	if !strings.Contains(sink.message, "operation completed") {
		t.Fatalf("syslog message does not include the log message: %q", sink.message)
	}
	if !strings.Contains(sink.message, `"component": "discovery"`) {
		t.Fatalf("syslog message does not include structured fields: %q", sink.message)
	}
	if !regexp.MustCompile(`\[log/log_test\.go:\d+\]`).MatchString(sink.message) {
		t.Fatalf("syslog message does not include the caller: %q", sink.message)
	}
}

func TestLegacyLevelFilteringAndConsoleMapping(t *testing.T) {
	previousLevel := GetLevel()
	SetLevel(WARNING)
	t.Cleanup(func() {
		SetLevel(previousLevel)
	})

	output := captureStderr(t, func() {
		Debugf("debug message")
		Infof("info message")
		Noticef("notice message")
		Warningf("warning message")
		Errorf("error message")
		Criticalf("critical message")
	})

	if strings.Contains(output, "debug message") || strings.Contains(output, "info message") || strings.Contains(output, "notice message") {
		t.Fatalf("output includes entries below WARNING: %q", output)
	}
	for _, expected := range []string{
		"\t[WARN]\t",
		"\t[ERROR]\t",
		"warning message",
		"error message",
		"critical message",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("output does not include %q: %q", expected, output)
		}
	}
}

func TestSyslogThresholdDistinguishesNoticeFromInfo(t *testing.T) {
	previousLevel := GetLevel()
	previousSyslogLevel := LogLevel(syslogLevel.Load())
	previousSyslogWriter := syslogWriter
	sink := &recordingSyslogSink{}
	SetLevel(DEBUG)
	SetSyslogLevel(NOTICE)
	syslogWriter = sink
	t.Cleanup(func() {
		syslogWriter = previousSyslogWriter
		SetSyslogLevel(previousSyslogLevel)
		SetLevel(previousLevel)
	})

	captureStderr(t, func() {
		Infof("info message")
	})
	if sink.priority != "" {
		t.Fatalf("INFO was written at NOTICE threshold with priority %q", sink.priority)
	}
	captureStderr(t, func() {
		Noticef("notice message")
	})
	if sink.priority != "notice" {
		t.Fatalf("NOTICE priority = %q; want %q", sink.priority, "notice")
	}
}

func TestConcurrentSetLevelAndLogging(t *testing.T) {
	previousLevel := GetLevel()
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	previousStderr := os.Stderr
	os.Stderr = devNull
	t.Cleanup(func() {
		os.Stderr = previousStderr
		SetLevel(previousLevel)
		if err := devNull.Close(); err != nil {
			t.Errorf("close %s: %v", os.DevNull, err)
		}
	})

	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 1_000; index++ {
			if index%2 == 0 {
				SetLevel(DEBUG)
			} else {
				SetLevel(ERROR)
			}
		}
	}()
	go func() {
		defer waitGroup.Done()
		for index := 0; index < 1_000; index++ {
			Infof("operation %d", index)
		}
	}()
	waitGroup.Wait()
}

func TestInfofWaitsForSyslogWrite(t *testing.T) {
	previousLevel := GetLevel()
	previousSyslogLevel := LogLevel(syslogLevel.Load())
	previousSyslogWriter := syslogWriter
	sink := newBlockingSyslogSink()
	SetLevel(INFO)
	SetSyslogLevel(INFO)
	syslogWriter = sink
	t.Cleanup(func() {
		syslogWriter = previousSyslogWriter
		SetSyslogLevel(previousSyslogLevel)
		SetLevel(previousLevel)
	})

	completed := make(chan struct{})
	go func() {
		Infof("operation completed")
		close(completed)
	}()

	select {
	case <-sink.started:
	case <-time.After(time.Second):
		t.Fatal("syslog write did not start")
	}
	select {
	case <-completed:
		t.Fatal("Infof returned before the syslog write completed")
	default:
	}

	close(sink.release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("Infof did not return after the syslog write completed")
	}
}

func TestSyslogWriteFailureIsVisibleOnStderr(t *testing.T) {
	previousLevel := GetLevel()
	previousSyslogLevel := LogLevel(syslogLevel.Load())
	previousSyslogWriter := syslogWriter
	SetLevel(INFO)
	SetSyslogLevel(INFO)
	syslogWriter = errorSyslogSink{err: errors.New("write unavailable")}
	t.Cleanup(func() {
		syslogWriter = previousSyslogWriter
		SetSyslogLevel(previousSyslogLevel)
		SetLevel(previousLevel)
	})

	output := captureStderr(t, func() {
		Infof("operation completed")
	})
	if !strings.Contains(output, "syslog write failed") {
		t.Fatalf("stderr does not report the syslog failure: %q", output)
	}
	if !strings.Contains(output, "write unavailable") {
		t.Fatalf("stderr does not include the syslog error: %q", output)
	}
}

func TestCloseClosesAndDisablesSyslog(t *testing.T) {
	previousSyslogWriter := syslogWriter
	sink := &closeTrackingSyslogSink{}
	syslogWriter = sink
	t.Cleanup(func() {
		syslogWriter = previousSyslogWriter
	})

	if err := Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if !sink.closed {
		t.Fatal("Close() did not close the syslog sink")
	}
	if syslogWriter != nil {
		t.Fatal("Close() left the syslog sink enabled")
	}
}

func TestCloseReturnsSyslogCloseFailure(t *testing.T) {
	previousSyslogWriter := syslogWriter
	expectedErr := errors.New("close unavailable")
	syslogWriter = errorSyslogSink{closeErr: expectedErr}
	t.Cleanup(func() {
		syslogWriter = previousSyslogWriter
	})

	if err := Close(); !errors.Is(err, expectedErr) {
		t.Fatalf("Close() error = %v; want wrapped %v", err, expectedErr)
	}
}

func TestCloseRunsRegisteredHooksAndReturnsFailure(t *testing.T) {
	expectedErr := errors.New("hook unavailable")
	called := false
	RegisterCloseHook(func() error {
		called = true
		return expectedErr
	})

	if err := Close(); !errors.Is(err, expectedErr) {
		t.Fatalf("Close() error = %v; want wrapped %v", err, expectedErr)
	}
	if !called {
		t.Fatal("Close() did not run the registered hook")
	}
}

func TestSyncReturnsCoreFailure(t *testing.T) {
	previousLogger := sugaredLogger
	expectedErr := errors.New("sync unavailable")
	sugaredLogger = zap.New(syncErrorCore{err: expectedErr}).Sugar()
	t.Cleanup(func() {
		sugaredLogger = previousLogger
	})

	if err := Sync(); !errors.Is(err, expectedErr) {
		t.Fatalf("Sync() error = %v; want wrapped %v", err, expectedErr)
	}
}

func TestConcurrentLoggingAndClose(t *testing.T) {
	previousLevel := GetLevel()
	previousSyslogLevel := LogLevel(syslogLevel.Load())
	previousSyslogWriter := syslogWriter
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	previousStderr := os.Stderr
	os.Stderr = devNull
	SetLevel(INFO)
	SetSyslogLevel(INFO)
	t.Cleanup(func() {
		os.Stderr = previousStderr
		syslogWriter = previousSyslogWriter
		SetSyslogLevel(previousSyslogLevel)
		SetLevel(previousLevel)
		if err := devNull.Close(); err != nil {
			t.Errorf("close %s: %v", os.DevNull, err)
		}
	})

	for attempt := 0; attempt < 100; attempt++ {
		syslogWriter = errorSyslogSink{}
		start := make(chan struct{})
		var waitGroup sync.WaitGroup
		waitGroup.Add(2)
		go func() {
			defer waitGroup.Done()
			<-start
			Infof("operation %d", attempt)
		}()
		go func() {
			defer waitGroup.Done()
			<-start
			_ = Close()
		}()
		close(start)
		waitGroup.Wait()
	}
}

func TestSyslogUsesLegacyPriorities(t *testing.T) {
	previousSyslogLevel := LogLevel(syslogLevel.Load())
	previousSyslogWriter := syslogWriter
	SetSyslogLevel(DEBUG)
	t.Cleanup(func() {
		syslogWriter = previousSyslogWriter
		SetSyslogLevel(previousSyslogLevel)
	})

	tests := []struct {
		name     string
		level    LogLevel
		priority string
	}{
		{name: "fatal", level: FATAL, priority: "emerg"},
		{name: "critical", level: CRITICAL, priority: "crit"},
		{name: "error", level: ERROR, priority: "err"},
		{name: "warning", level: WARNING, priority: "warning"},
		{name: "notice", level: NOTICE, priority: "notice"},
		{name: "info", level: INFO, priority: "info"},
		{name: "debug", level: DEBUG, priority: "debug"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sink := &recordingSyslogSink{}
			syslogWriter = sink
			if err := writeSyslog(test.level, "operation completed"); err != nil {
				t.Fatalf("writeSyslog() error: %v", err)
			}
			if sink.priority != test.priority {
				t.Fatalf("priority = %q; want %q", sink.priority, test.priority)
			}
			if sink.message != "operation completed" {
				t.Fatalf("message = %q; want %q", sink.message, "operation completed")
			}
		})
	}
}

func TestRealSyslogSmoke(t *testing.T) {
	if os.Getenv("ORCHESTRATOR_REAL_SYSLOG") != "1" {
		t.Skip("set ORCHESTRATOR_REAL_SYSLOG=1 to use the host syslog service")
	}
	marker := os.Getenv("ORCHESTRATOR_REAL_SYSLOG_MARKER")
	if marker == "" {
		t.Fatal("ORCHESTRATOR_REAL_SYSLOG_MARKER is required")
	}
	previousLevel := GetLevel()
	previousSyslogLevel := LogLevel(syslogLevel.Load())
	SetLevel(DEBUG)
	SetSyslogLevel(DEBUG)
	t.Cleanup(func() {
		_ = Close()
		SetSyslogLevel(previousSyslogLevel)
		SetLevel(previousLevel)
	})
	if err := EnableSyslogWriter("orchestrator"); err != nil {
		t.Fatalf("EnableSyslogWriter() error: %v", err)
	}

	Debugf("%s-debug", marker)
	Infof("%s-info", marker)
	Noticef("%s-notice", marker)
	Warningf("%s-warning", marker)
	Errorf("%s-error", marker)
	Criticalf("%s-critical", marker)
	if err := Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestRealSyslogFatalHelperProcess(t *testing.T) {
	if os.Getenv("ORCHESTRATOR_REAL_SYSLOG_FATAL") != "1" {
		return
	}
	marker := os.Getenv("ORCHESTRATOR_REAL_SYSLOG_MARKER")
	if marker == "" {
		t.Fatal("ORCHESTRATOR_REAL_SYSLOG_MARKER is required")
	}
	SetLevel(DEBUG)
	SetSyslogLevel(DEBUG)
	if err := EnableSyslogWriter("orchestrator"); err != nil {
		t.Fatalf("EnableSyslogWriter() error: %v", err)
	}
	Fatalf("%s-fatal", marker)
}

type blockingSyslogSink struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingSyslogSink() *blockingSyslogSink {
	return &blockingSyslogSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *blockingSyslogSink) write(string) error {
	s.once.Do(func() {
		close(s.started)
	})
	<-s.release
	return nil
}

func (s *blockingSyslogSink) Emerg(message string) error   { return s.write(message) }
func (s *blockingSyslogSink) Crit(message string) error    { return s.write(message) }
func (s *blockingSyslogSink) Err(message string) error     { return s.write(message) }
func (s *blockingSyslogSink) Warning(message string) error { return s.write(message) }
func (s *blockingSyslogSink) Notice(message string) error  { return s.write(message) }
func (s *blockingSyslogSink) Info(message string) error    { return s.write(message) }
func (s *blockingSyslogSink) Debug(message string) error   { return s.write(message) }
func (s *blockingSyslogSink) Close() error                 { return nil }

type errorSyslogSink struct {
	err      error
	closeErr error
}

func (s errorSyslogSink) Emerg(string) error   { return s.err }
func (s errorSyslogSink) Crit(string) error    { return s.err }
func (s errorSyslogSink) Err(string) error     { return s.err }
func (s errorSyslogSink) Warning(string) error { return s.err }
func (s errorSyslogSink) Notice(string) error  { return s.err }
func (s errorSyslogSink) Info(string) error    { return s.err }
func (s errorSyslogSink) Debug(string) error   { return s.err }
func (s errorSyslogSink) Close() error         { return s.closeErr }

type syncErrorCore struct {
	err error
}

func (syncErrorCore) Enabled(zapcore.Level) bool { return false }
func (core syncErrorCore) With([]zapcore.Field) zapcore.Core {
	return core
}
func (syncErrorCore) Check(entry zapcore.Entry, checkedEntry *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return checkedEntry
}
func (syncErrorCore) Write(zapcore.Entry, []zapcore.Field) error { return nil }
func (core syncErrorCore) Sync() error                           { return core.err }

type closeTrackingSyslogSink struct {
	closed bool
}

func (*closeTrackingSyslogSink) Emerg(string) error   { return nil }
func (*closeTrackingSyslogSink) Crit(string) error    { return nil }
func (*closeTrackingSyslogSink) Err(string) error     { return nil }
func (*closeTrackingSyslogSink) Warning(string) error { return nil }
func (*closeTrackingSyslogSink) Notice(string) error  { return nil }
func (*closeTrackingSyslogSink) Info(string) error    { return nil }
func (*closeTrackingSyslogSink) Debug(string) error   { return nil }
func (s *closeTrackingSyslogSink) Close() error {
	s.closed = true
	return nil
}

type recordingSyslogSink struct {
	priority string
	message  string
}

func (s *recordingSyslogSink) record(priority, message string) error {
	s.priority = priority
	s.message = message
	return nil
}

func (s *recordingSyslogSink) Emerg(message string) error {
	return s.record("emerg", message)
}
func (s *recordingSyslogSink) Crit(message string) error {
	return s.record("crit", message)
}
func (s *recordingSyslogSink) Err(message string) error {
	return s.record("err", message)
}
func (s *recordingSyslogSink) Warning(message string) error {
	return s.record("warning", message)
}
func (s *recordingSyslogSink) Notice(message string) error {
	return s.record("notice", message)
}
func (s *recordingSyslogSink) Info(message string) error {
	return s.record("info", message)
}
func (s *recordingSyslogSink) Debug(message string) error {
	return s.record("debug", message)
}
func (*recordingSyslogSink) Close() error { return nil }

type markerSyslogSink struct {
	path string
}

func (markerSyslogSink) Emerg(string) error   { return nil }
func (markerSyslogSink) Crit(string) error    { return nil }
func (markerSyslogSink) Err(string) error     { return nil }
func (markerSyslogSink) Warning(string) error { return nil }
func (markerSyslogSink) Notice(string) error  { return nil }
func (markerSyslogSink) Info(string) error    { return nil }
func (markerSyslogSink) Debug(string) error   { return nil }
func (sink markerSyslogSink) Close() error {
	return os.WriteFile(sink.path, []byte("closed\n"), 0600)
}

func captureStderr(t *testing.T, write func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	previousStderr := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = previousStderr
	}()

	write()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return string(output)
}

func captureStdout(t *testing.T, write func()) string {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	previousStdout := os.Stdout
	os.Stdout = writer
	defer func() {
		os.Stdout = previousStdout
	}()

	write()
	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return string(output)
}
