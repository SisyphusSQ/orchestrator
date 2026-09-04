package inst

import "testing"

func TestFirstClusterAliasReturnsAliasInsteadOfClusterName(t *testing.T) {
	rows := []clusterAliasRow{{Alias: "payments"}}
	if got := firstClusterAlias(rows); got != "payments" {
		t.Fatalf("firstClusterAlias() = %q; want payments", got)
	}
	if got := firstClusterAlias(nil); got != "" {
		t.Fatalf("firstClusterAlias(nil) = %q; want empty", got)
	}
}
