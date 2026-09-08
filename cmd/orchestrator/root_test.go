package main

import (
	"bytes"
	"testing"
)

func TestServerCommandBoundaries(t *testing.T) {
	for _, args := range [][]string{{"clusters"}, {"-c", "clusters"}, {"http"}, {"cli"}, {"server", "-config", "server.json"}, {"server", "-discovery"}, {"--ignore-raft-setup"}, {"admin", "access-token"}} {
		var output bytes.Buffer
		called := false
		err := execute(args, &output, &output, func(*commandOptions, string) error { called = true; return nil })
		if err == nil || called {
			t.Errorf("%v error=%v called=%v", args, err, called)
		}
	}
}
func TestServerOfflineHelpAndLocalCommands(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help", "server"}, {"completion", "zsh"}, {"--version"}} {
		var output bytes.Buffer
		err := execute(args, &output, &output, func(*commandOptions, string) error { t.Fatal("runtime initialized"); return nil })
		if err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	err := execute([]string{"server", "--config", "server.json", "--discovery=false"}, &output, &output, func(o *commandOptions, name string) error {
		if name != "server" || o.discovery || o.configFile != "server.json" || o.runtime.Noop == nil {
			t.Fatal("incorrect server options")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
