package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOfflineCommands(t *testing.T) {
	t.Setenv("ORCH_TIMEOUT", "invalid")
	t.Setenv("ORCH_ENDPOINT", "invalid")
	for _, args := range [][]string{{"--help"}, {"--version"}, {"help", "recover"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"completion", "powershell"}} {
		var out, errout bytes.Buffer
		if err := Execute(t.Context(), args, &out, &errout, "1", "commit"); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if out.Len() == 0 {
			t.Fatalf("%v produced no output", args)
		}
	}
}
func TestInvalidArgumentsNeverSend(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer s.Close()
	for _, args := range [][]string{{"-c", "clusters"}, {"cli"}, {"relocate-below"}, {"stop-slave"}, {"discover", "-instance", "db"}, {"relocate", "-i", "db", "-s", "dest"}, {"clusters", "-output", "json"}, {"discover"}, {"relocate", "-i", "db:bad", "-d", "dest"}, {"discover", "--bogus"}, {"discover", "-i", "db,other:bad"}, {"register-candidate", "-i", "db", "--promotion-rule", "bad"}, {"raft-add-member", "--body", "{"}} {
		var out bytes.Buffer
		err := Execute(t.Context(), append([]string{"--endpoint", s.URL}, args...), &out, &out, "", "")
		if err == nil || ExitCode(err) != 2 {
			t.Errorf("%v: %v (exit %d)", args, err, ExitCode(err))
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid arguments caused requests")
	}
}
func TestRecoverHasExactlyOneRequestWithDestination(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/recover/db/3306/candidate/3307" {
			t.Errorf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"Code":"OK","Details":{"Hostname":"candidate","Port":3307}}`))
	}))
	defer s.Close()
	var out, errout bytes.Buffer
	err := Execute(t.Context(), []string{"--endpoint", s.URL, "recover", "-i", "db", "-d", "candidate:3307", "--output", "json"}, &out, &errout, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || !json.Valid(out.Bytes()) || errout.Len() != 0 {
		t.Fatalf("calls=%d stdout=%s stderr=%s", calls.Load(), &out, &errout)
	}
}
func TestCatalogPrepareEveryCommand(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range catalog() {
		if seen[spec.Name] {
			t.Fatal("duplicate", spec.Name)
		}
		seen[spec.Name] = true
		values := map[string]string{"instance": "[::1]:3306", "destination": "target:3307", "owner": "operator", "reason": "change & verify", "duration": "10m", "hostname": "virtual", "promotion-rule": "prefer", "binlog": "mysql-bin.000001:4", "pool": "pool", "tag": "tier=primary", "search": "host", "pattern": "db.*", "seconds": "10", "statement": "SET GLOBAL x=1", "id": "node-2", "body": "{}", "instances": "db:3306"}
		if strings.Contains(spec.Path, "{cluster") {
			values["instance"] = ""
			values["cluster"] = "cluster-a"
		}
		if _, err := prepare(spec, values); err != nil {
			t.Errorf("%s: %v", spec.Name, err)
		}
	}
}
func TestBatchPartialFailure(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.Path, "second") {
			_, _ = w.Write([]byte(`{"Code":"ERROR","Message":"failed"}`))
			return
		}
		_, _ = w.Write([]byte(`{"Code":"OK","Details":"first"}`))
	}))
	defer s.Close()
	var out bytes.Buffer
	err := Execute(t.Context(), []string{"--endpoint", s.URL, "discover", "-i", "first,second,third", "--output", "json"}, &out, &out, "", "")
	if ExitCode(err) != 4 || calls.Load() != 2 || !json.Valid(out.Bytes()) {
		t.Fatalf("err=%v calls=%d output=%s", err, calls.Load(), &out)
	}
}
func TestProjectionDoesNotLoseIntegers(t *testing.T) {
	body, err := project(json.RawMessage(`{"Code":"OK","Details":{"position":9007199254740993}}`), "")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := render(&out, body); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "9007199254740993") {
		t.Fatal(out.String())
	}
}
func TestEndpointFlagOverridesEnvironment(t *testing.T) {
	t.Setenv("ORCH_ENDPOINT", "http://invalid:1")
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) }))
	defer s.Close()
	var out bytes.Buffer
	if err := Execute(t.Context(), []string{"--endpoint", s.URL, "clusters"}, &out, &out, "", ""); err != nil {
		t.Fatal(err)
	}
}

func TestCommandParametersReachHTTP(t *testing.T) {
	for _, tc := range []struct {
		args        []string
		path, query string
	}{
		{[]string{"search", "--search", "a & b"}, "/api/search", "s=a+%26+b"},
		{[]string{"repoint", "-i", "db"}, "/api/repoint/db/3306", ""},
		{[]string{"repoint-replicas", "-i", "db", "-d", "target:3307"}, "/api/repoint-replicas/db/3306", "destination=target%3A3307"},
		{[]string{"flush-binary-logs", "-i", "db", "--binlog", "mysql-bin.000009"}, "/api/flush-binary-logs/db/3306", "binlog=mysql-bin.000009"},
		{[]string{"begin-maintenance", "-i", "db", "--owner", "operator", "--reason", "planned", "--duration", "2h"}, "/api/begin-maintenance/db/3306/operator/planned", "duration=2h"},
		{[]string{"last-pseudo-gtid", "-i", "db", "--strict"}, "/api/last-pseudo-gtid/db/3306", "strict=true"},
		{[]string{"replication-analysis"}, "/api/replication-analysis", "includeDowntimed=false"},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			var calls int
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != tc.path || r.URL.RawQuery != tc.query {
					t.Errorf("unexpected request: %s", r.URL)
				}
				_, _ = w.Write([]byte(`{"Code":"OK","Details":[]}`))
			}))
			defer s.Close()
			var output bytes.Buffer
			err := Execute(t.Context(), append([]string{"--endpoint", s.URL}, tc.args...), &output, &output, "", "")
			if err != nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestMalformedValuesFailBeforeAnyRequest(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer s.Close()
	for _, args := range [][]string{
		{"begin-maintenance", "-i", "db", "--owner", "o", "--reason", "r", "--duration", "-1h"},
		{"begin-maintenance", "-i", "db", "--owner", "o", "--reason", "r", "--duration", "999999999999999999999999w"},
		{"correlate-binlog-pos", "-i", "db", "-d", "dest", "--binlog", "file:-1"},
		{"last-pseudo-gtid", "-i", "db", "--strict=maybe"},
		{"repoint-replicas", "-i", "db", "-d", "dest:99999"},
	} {
		var output bytes.Buffer
		err := Execute(t.Context(), append([]string{"--endpoint", s.URL}, args...), &output, &output, "", "")
		if ExitCode(err) != 2 {
			t.Errorf("%v: %v", args, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid arguments sent a request")
	}
}
