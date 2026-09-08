package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
)

func TestCommandDispatchAndAliases(t *testing.T) {
	for _, definition := range commandDefinitions() {
		for _, name := range append([]string{definition.Command}, definition.Aliases...) {
			for _, args := range [][]string{{name}, {"-c", name}, {"-c", name, "cli"}} {
				t.Run(strings.Join(args, " "), func(t *testing.T) {
					calls := 0
					err := execute(args, io.Discard, io.Discard, func(_ *commandOptions, command string) error {
						calls++
						if command != definition.Command {
							t.Fatalf("command = %q; want %q", command, definition.Command)
						}
						return nil
					})
					if err != nil || calls != 1 {
						t.Fatalf("error = %v, calls = %d; want nil, 1", err, calls)
					}
				})
			}
		}
	}
}

func TestLegacyAndNativeOptionsAgree(t *testing.T) {
	common := []string{
		"--config", "example.json", "--strict", "--owner", "owner", "--reason", "-debug",
		"--duration", "4w", "--pattern", "-config", "--alias", "cluster", "--pool", "pool",
		"--hostname", "vip", "--discovery=false", "--quiet", "--verbose", "--debug", "--stack",
		"--skip-binlog-search", "--skip-unresolve", "--skip-unresolve-check", "--noop",
		"--binlog", "mysql-bin.000001", "--statement", "--", "--grab-election",
		"--promotion-rule", "must_not", "--skip-continuous-registration", "--enable-database-update",
		"--ignore-raft-setup", "--tag", "a=1,b",
	}
	legacy := []string{"-c", "relocate", "-i", "db1:3306, db2:3306", "-s", "db3:3306"}
	native := []string{"relocate", "--instance", "db1:3306, db2:3306", "--destination", "db3:3306"}
	var previous *commandOptions
	for _, prefix := range [][]string{legacy, native} {
		args := append(append([]string{}, prefix...), common...)
		err := execute(args, io.Discard, io.Discard, func(options *commandOptions, command string) error {
			if command != "relocate" || options.instance != "db1:3306, db2:3306" || options.destination != "db3:3306" {
				t.Fatalf("wrong command/instances/destination: %q/%q/%q", command, options.instance, options.destination)
			}
			if options.reason != "-debug" || options.pattern != "-config" || *options.runtime.Statement != "--" {
				t.Fatal("flag-like argument values were rewritten")
			}
			options.command, options.sibling = "", ""
			if previous != nil && !reflect.DeepEqual(previous, options) {
				t.Fatalf("native options differ from legacy: %+v / %+v", options, previous)
			}
			previous = options
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestLegacyFlagSpellingsAndPositions(t *testing.T) {
	for _, args := range [][]string{
		{"-config=example.json", "-c=discover", "-i=db:3306", "-debug=true"},
		{"--c", "discover", "--i", "db:3306", "--config", "example.json", "--debug"},
		{"--config", "example.json", "discover", "--i=db:3306", "-debug"},
		{"cli", "-c", "discover", "-config", "example.json", "-i", "db:3306", "-debug"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			err := execute(args, io.Discard, io.Discard, func(options *commandOptions, command string) error {
				calls++
				if command != "discover" || options.configFile != "example.json" || options.instance != "db:3306" || !options.debug {
					t.Fatalf("unexpected options: %+v, command %q", options, command)
				}
				return nil
			})
			if err != nil || calls != 1 {
				t.Fatalf("error = %v, calls = %d", err, calls)
			}
		})
	}
}

func TestInformationalCommandsSkipRuntime(t *testing.T) {
	for _, args := range [][]string{
		{}, {"help"}, {"-h"}, {"-help"}, {"--help"}, {"-c", "help"}, {"-c", "help", "cli"},
		{"help", "relocate"}, {"help", "stop-slave"}, {"help", "http"}, {"help", "completion"},
		{"-c", "relocate", "help"}, {"relocate", "--help"}, {"--version"}, {"-version"}, {"http", "--version"},
		{"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"},
		{"__complete", "reloc"}, {"__complete", "relocate", "--"},
		{"__complete", "relocate", "-pro"}, {"__completeNoDesc", "reloc"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out bytes.Buffer
			before := config.RuntimeCLIFlags
			args = append([]string{"--config", filepath.Join(t.TempDir(), "missing.json")}, args...)
			err := execute(args, &out, io.Discard, func(_ *commandOptions, _ string) error {
				t.Fatal("informational command attempted runtime initialization")
				return nil
			})
			if err != nil || out.Len() == 0 {
				t.Fatalf("error = %v, output length = %d", err, out.Len())
			}
			if !reflect.DeepEqual(before, config.RuntimeCLIFlags) {
				t.Fatal("informational command modified process runtime flags")
			}
		})
	}
}

func TestInvalidCommandsNeverRun(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"not-a-command"}, "unknown command"},
		{[]string{"-c", "not-a-command"}, "unknown command"},
		{[]string{"-c", ""}, "unknown command"},
		{[]string{"-c", "clusters", "discover"}, "cannot combine"},
		{[]string{"discover", "-c", "discover"}, "cannot combine"},
		{[]string{"http", "-c", "clusters"}, "cannot combine"},
		{[]string{"completion", "bash", "-c", "clusters"}, "cannot combine"},
		{[]string{"discover", "extra"}, "unknown command"},
		{[]string{"discover", "--", "--debug"}, "unknown command"},
		{[]string{"discover", "--unknown"}, "unknown flag"},
		{[]string{"discover", "-debugg"}, "unknown flag"},
		{[]string{"discover", "--promotion-rule=invalid"}, "promotion-rule"},
		{[]string{"discover", "-s", "db1", "-d", "db2"}, "synonyms"},
		{[]string{"discover", "--debug=invalid"}, "invalid"},
		{[]string{"-c"}, "needs an argument"},
		{[]string{"help", "unknown"}, "unknown help topic"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			err := execute(tc.args, io.Discard, io.Discard, func(_ *commandOptions, _ string) error {
				t.Fatal("invalid command reached execution")
				return nil
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %q", err, tc.want)
			}
		})
	}
}

func TestCommandDefaultsAndIsolation(t *testing.T) {
	var first *commandOptions
	for i, args := range [][]string{{"http", "--discovery=false", "--noop", "--debug"}, {"http"}} {
		err := execute(args, io.Discard, io.Discard, func(options *commandOptions, command string) error {
			if command != "http" {
				t.Fatalf("command = %q", command)
			}
			if i == 0 {
				first = options
			} else if !options.discovery || *options.runtime.Noop || options.debug || *options.runtime.PromotionRule != "prefer" || first.runtime.Noop == options.runtime.Noop {
				t.Fatalf("options not independently initialized: %+v", options)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestVersionOutputAndExecutionErrors(t *testing.T) {
	oldVersion, oldCommit := AppVersion, GitCommit
	t.Cleanup(func() { AppVersion, GitCommit = oldVersion, oldCommit })
	AppVersion, GitCommit = "1.2.3", "abc123"
	for _, args := range [][]string{{"--version"}, {"--version", "help"}, {"http", "--version"}, {"completion", "bash", "--version"}} {
		var out bytes.Buffer
		if err := execute(args, &out, io.Discard, nil); err != nil {
			t.Fatal(err)
		}
		if out.String() != "1.2.3\nabc123\n" {
			t.Fatalf("%v version = %q", args, out.String())
		}
	}
	want := errors.New("operation failed")
	err := execute([]string{"discover"}, io.Discard, io.Discard, func(_ *commandOptions, _ string) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("error = %v; want original error", err)
	}
}

func TestRuntimeConfigurationBinding(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "orchestrator.json")
	contents, err := json.Marshal(map[string]any{
		"BackendDB": "sqlite", "SQLite3DataFile": filepath.Join(directory, "orchestrator.db"),
		"HostnameResolveMethod": "none", "SkipOrchestratorDatabaseUpdate": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, contents, 0600); err != nil {
		t.Fatal(err)
	}
	var previous map[string]any
	for _, invocation := range [][]string{{"dump-config"}, {"-c", "dump-config", "cli"}} {
		args := append([]string{"-test.run=^TestMainCLIHelper$", "--", "--config", configPath, "--enable-database-update"}, invocation...)
		command := exec.Command(os.Args[0], args...)
		command.Env = append(os.Environ(), "GO_WANT_MAIN_CLI_HELPER=1")
		var out, stderr bytes.Buffer
		command.Stdout, command.Stderr = &out, &stderr
		if err := command.Run(); err != nil {
			t.Fatalf("dump-config: %v, stderr %s", err, &stderr)
		}
		var got map[string]any
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatalf("dump-config did not produce JSON: %v", err)
		}
		if got["SQLite3DataFile"] != filepath.Join(directory, "orchestrator.db") || got["SkipOrchestratorDatabaseUpdate"] != false {
			t.Fatal("configuration file or enable-database-update flag did not reach runtime")
		}
		if previous != nil && !reflect.DeepEqual(previous, got) {
			t.Fatal("native and legacy invocations produced different configuration")
		}
		previous = got
		if strings.Count(stderr.String(), "cleanup-marker") != 1 {
			t.Fatal("successful runtime command did not run cleanup exactly once")
		}
	}
}

func TestMainExitAndCleanup(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"--help"}, 0, ""},
		{[]string{"help", "discover"}, 0, ""},
		{[]string{"--version"}, 0, ""},
		{[]string{"completion", "zsh"}, 0, ""},
		{[]string{"not-a-command"}, 1, "unknown command"},
		{[]string{"discover"}, 1, "load configuration"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append([]string{"-test.run=^TestMainCLIHelper$", "--", "--config", filepath.Join(t.TempDir(), "missing.json")}, tc.args...)
			command := exec.Command(os.Args[0], args...)
			command.Env = append(os.Environ(), "GO_WANT_MAIN_CLI_HELPER=1")
			var out, stderr bytes.Buffer
			command.Stdout, command.Stderr = &out, &stderr
			err := command.Run()
			code := 0
			if err != nil {
				var exitErr *exec.ExitError
				if !errors.As(err, &exitErr) {
					t.Fatal(err)
				}
				code = exitErr.ExitCode()
			}
			if code != tc.code || !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("code = %d, stderr = %q; want %d, %q", code, stderr.String(), tc.code, tc.want)
			}
			if strings.Count(stderr.String(), "cleanup-marker") != 1 {
				t.Fatalf("cleanup did not run exactly once: %q", stderr.String())
			}
			if tc.code != 0 && out.Len() != 0 {
				t.Fatalf("failed command polluted stdout: %q", out.String())
			}
		})
	}
}

func TestMainCLIHelper(t *testing.T) {
	if os.Getenv("GO_WANT_MAIN_CLI_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"orchestrator"}, os.Args[i+1:]...)
			break
		}
	}
	log.RegisterCloseHook(func() error {
		fmt.Fprintln(os.Stderr, "cleanup-marker")
		return nil
	})
	main()
	os.Exit(0)
}
