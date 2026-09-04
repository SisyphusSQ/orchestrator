package orcraft

import (
	"errors"
	"testing"
	"time"

	"github.com/hashicorp/raft"
)

func TestClassOfDistinguishesErrorCategories(t *testing.T) {
	tests := []struct {
		err  error
		want Class
	}{
		{err: nil, want: ""},
		{err: ErrNotEnabled, want: ClassDisabled},
		{err: ErrNotLeader, want: ClassNotLeader},
		{err: ErrNotBootstrapped, want: ClassNotBootstrapped},
		{err: ErrAlreadyBootstrapped, want: ClassConflict},
		{err: ErrStaleConfiguration, want: ClassConflict},
		{err: ErrIdentityConflict, want: ClassConflict},
		{err: ErrNotFound, want: ClassNotFound},
		{err: ErrConfigurationInProgress, want: ClassIndeterminate},
		{err: ErrIndeterminate, want: ClassIndeterminate},
		{err: ErrInvalidArgument, want: ClassInvalidArgument},
		{err: raft.ErrNotLeader, want: ClassNotLeader},
		{err: raft.ErrLeadershipLost, want: ClassIndeterminate},
		{err: raft.ErrLeadershipTransferInProgress, want: ClassConflict},
		{err: raft.ErrCantBootstrap, want: ClassConflict},
		{err: raft.ErrEnqueueTimeout, want: ClassFailed},
		{err: errors.New("configuration changed since 3 (latest is 4)"), want: ClassConflict},
	}
	for _, tc := range tests {
		if got := ClassOf(tc.err); got != tc.want {
			t.Fatalf("ClassOf(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestClassifyLeadershipTransferErrorTreatsRuntimeFailuresAsIndeterminate(t *testing.T) {
	if got := ClassOf(classifyLeadershipTransferError(errors.New("leadership transfer timeout"))); got != ClassIndeterminate {
		t.Fatalf("leadership transfer timeout class = %q, want %q", got, ClassIndeterminate)
	}
	if got := ClassOf(classifyLeadershipTransferError(errors.New("failed to make TimeoutNow RPC"))); got != ClassIndeterminate {
		t.Fatalf("leadership transfer RPC error class = %q, want %q", got, ClassIndeterminate)
	}
	if got := ClassOf(classifyLeadershipTransferError(raft.ErrEnqueueTimeout)); got != ClassFailed {
		t.Fatalf("leadership transfer enqueue timeout class = %q, want %q", got, ClassFailed)
	}
}

func TestMapConfigurationAndSuffrage(t *testing.T) {
	cfg := raft.Configuration{
		Servers: []raft.Server{
			{ID: "n1", Address: "127.0.0.1:1", Suffrage: raft.Voter},
			{ID: "n2", Address: "127.0.0.1:2", Suffrage: raft.Nonvoter},
		},
	}
	view := mapConfiguration(cfg, 7)
	if view.Index != 7 || len(view.Servers) != 2 {
		t.Fatalf("unexpected view: %+v", view)
	}
	if view.Servers[0].Suffrage != suffrageVoter || view.Servers[1].Suffrage != suffrageNonvoter {
		t.Fatalf("suffrage mapping = %+v", view.Servers)
	}
	if _, err := parseSuffrage("voter"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSuffrage("nonvoter"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseSuffrage("non-voter"); err == nil {
		t.Fatal("undocumented non-voter suffrage succeeded")
	}
	if _, err := parseSuffrage("candidate"); err == nil {
		t.Fatal("expected invalid suffrage error")
	}
}

func TestMembershipConflictDetection(t *testing.T) {
	cfg := raft.Configuration{
		Servers: []raft.Server{{ID: "n1", Address: "127.0.0.1:1", Suffrage: raft.Voter}},
	}
	if err := membershipConflict(cfg, "n1", "127.0.0.1:2"); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("same id different address: %v", err)
	}
	if err := membershipConflict(cfg, "n2", "127.0.0.1:1"); !errors.Is(err, ErrIdentityConflict) {
		t.Fatalf("same address different id: %v", err)
	}
	if err := membershipConflict(cfg, "n1", "127.0.0.1:1"); err != nil {
		t.Fatalf("same id same address: %v", err)
	}
	if err := membershipConflict(cfg, "n2", "127.0.0.1:2"); err != nil {
		t.Fatalf("new member: %v", err)
	}
}

func TestFollowerLastContactThreshold(t *testing.T) {
	got := FollowerLastContactThreshold(time.Second)
	if got != 10*time.Second {
		t.Fatalf("threshold = %s, want 10s poll window", got)
	}
	got = FollowerLastContactThreshold(5 * time.Second)
	if got != 15*time.Second {
		t.Fatalf("threshold = %s, want 3*electionTimeout", got)
	}
}

func TestConfigurationCommitted(t *testing.T) {
	if configurationCommitted(false, 0, 0) {
		t.Fatal("empty configuration reported committed")
	}
	if configurationCommitted(true, 8, 7) {
		t.Fatal("configuration ahead of commit index reported committed")
	}
	if configurationCommitted(true, 0, 0) {
		t.Fatal("unversioned configuration reported committed")
	}
	if !configurationCommitted(true, 8, 8) || !configurationCommitted(true, 8, 9) {
		t.Fatal("committed configuration reported uncommitted")
	}
}
