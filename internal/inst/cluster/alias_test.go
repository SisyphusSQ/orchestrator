package cluster

import (
	"testing"

	modeldomain "github.com/openark/orchestrator/internal/models/domain"
)

func TestFirstClusterAliasReturnsAliasInsteadOfClusterName(t *testing.T) {
	rows := []modeldomain.ClusterAlias{{Alias: "payments"}}
	if got := firstClusterAlias(rows); got != "payments" {
		t.Fatalf("firstClusterAlias() = %q; want payments", got)
	}
	if got := firstClusterAlias(nil); got != "" {
		t.Fatalf("firstClusterAlias(nil) = %q; want empty", got)
	}
}
