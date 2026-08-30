package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/openark/golib/log"
)

func TestConfigureSyslogReturnsInitializationError(t *testing.T) {
	expectedErr := errors.New("syslog unavailable")
	calledTag := ""

	err := configureSyslog(true, func(tag string) error {
		calledTag = tag
		return expectedErr
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("configureSyslog() error = %v; want wrapped %v", err, expectedErr)
	}
	if calledTag != "orchestrator" {
		t.Fatalf("syslog tag = %q; want %q", calledTag, "orchestrator")
	}
}

func TestConfigureSyslogDoesNothingWhenDisabled(t *testing.T) {
	called := false
	err := configureSyslog(false, func(string) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("configureSyslog() error: %v", err)
	}
	if called {
		t.Fatal("configureSyslog() initialized syslog while disabled")
	}
}

func TestEnabledSyslogInitializationFailureExitsProcess(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestSyslogStartupFailureHelperProcess$")
	command.Env = append(os.Environ(), "GO_WANT_SYSLOG_STARTUP_FAILURE_HELPER=1")
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("startup helper error = %v; want process exit error", err)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("startup helper exit code = %d; want 1", exitErr.ExitCode())
	}
	if stdout.Len() != 0 {
		t.Fatalf("startup helper stdout = %q; want empty output", stdout.String())
	}
	if !strings.Contains(stderr.String(), "initialize syslog: syslog unavailable") {
		t.Fatalf("startup helper stderr does not explain the failure: %q", stderr.String())
	}
}

func TestSyslogStartupFailureHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_SYSLOG_STARTUP_FAILURE_HELPER") != "1" {
		return
	}
	if err := configureSyslog(true, func(string) error {
		return errors.New("syslog unavailable")
	}); err != nil {
		log.Fatalf("%v", err)
	}
}
