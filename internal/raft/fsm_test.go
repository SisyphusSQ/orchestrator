package orcraft

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/raft"
)

func TestFSMApplyDispatchesBusinessCommands(t *testing.T) {
	app := &memoryApp{}
	store := &Store{applier: app}
	fsm := (*fsm)(store)

	for _, op := range []string{"heartbeat", "discover", "begin-downtime"} {
		payload, err := json.Marshal(&storeCommand{Op: op, Value: []byte(`{"x":1}`)})
		if err != nil {
			t.Fatalf("marshal %s: %v", op, err)
		}
		if got := fsm.Apply(&raft.Log{Index: 1, Data: payload}); got != nil {
			t.Fatalf("Apply(%s) = %v, want nil", op, got)
		}
	}

	ops := app.commands()
	want := []string{"heartbeat", "discover", "begin-downtime"}
	if strings.Join(ops, ",") != strings.Join(want, ",") {
		t.Fatalf("applied ops = %v, want %v", ops, want)
	}
}

func TestFSMApplyRejectsInvalidJSON(t *testing.T) {
	app := &memoryApp{}
	store := &Store{applier: app}
	fsm := (*fsm)(store)
	if got := fsm.Apply(&raft.Log{Index: 1, Data: []byte("{")}); got == nil {
		t.Fatal("Apply(invalid json) = nil, want error")
	}
	if len(app.commands()) != 0 {
		t.Fatalf("invalid json was applied: %v", app.commands())
	}
}
