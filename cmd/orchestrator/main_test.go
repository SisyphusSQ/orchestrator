package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/golib/log"
)

func TestResolveDefaultConfigurationFiles(t *testing.T) {
	root := t.TempDir()
	systemBase := filepath.Join(root, "etc", "orchestrator.conf")
	repositoryBase := filepath.Join(root, "conf", "orchestrator.conf")
	workingBase := filepath.Join(root, "orchestrator.conf")
	for _, directory := range []string{filepath.Dir(systemBase), filepath.Dir(repositoryBase)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	systemFile := systemBase + ".json"
	repositoryFile := repositoryBase + ".yaml"
	workingFile := workingBase + ".yml"
	for _, fileName := range []string{systemFile, repositoryFile, workingFile} {
		if err := os.WriteFile(fileName, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := resolveDefaultConfigurationFiles([]string{systemBase, repositoryBase, workingBase})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{systemFile, repositoryFile, workingFile}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved files = %v; want %v", got, want)
	}
}

func TestResolveDefaultConfigurationFilesRejectsAmbiguousFormats(t *testing.T) {
	base := filepath.Join(t.TempDir(), "orchestrator.conf")
	for _, extension := range []string{".json", ".yaml"} {
		if err := os.WriteFile(base+extension, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	_, err := resolveDefaultConfigurationFiles([]string{base})
	if err == nil || !strings.Contains(err.Error(), "ambiguous configuration") {
		t.Fatalf("resolve ambiguous configuration error = %v", err)
	}
}

func TestResolveDefaultConfigurationFilesAllowsNoFiles(t *testing.T) {
	files, err := resolveDefaultConfigurationFiles([]string{filepath.Join(t.TempDir(), "orchestrator.conf")})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("resolved files = %v; want none", files)
	}
}

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

func TestRegisterProcessCloseHooksRegistersAuditAndDatabaseCleanup(t *testing.T) {
	var hooks []func() error
	register := func(hook func() error) {
		hooks = append(hooks, hook)
	}
	auditClosed := false
	databaseClosed := false
	registerProcessCloseHooks(
		register,
		func() error {
			auditClosed = true
			return nil
		},
		func() error {
			databaseClosed = true
			return nil
		},
	)

	if len(hooks) != 2 {
		t.Fatalf("registered close hooks = %d; want 2", len(hooks))
	}
	for _, hook := range hooks {
		if err := hook(); err != nil {
			t.Fatalf("close hook error: %v", err)
		}
	}
	if !auditClosed || !databaseClosed {
		t.Fatalf("cleanup state audit/database = %t/%t; want true/true", auditClosed, databaseClosed)
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
