package attributes

import (
	"testing"

	"github.com/openark/orchestrator/internal/models/domain"
)

func TestHostAttributeValueReturnsErrorForMissingRow(t *testing.T) {
	deferred := func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("hostAttributeValue() panicked for a missing row: %v", recovered)
		}
	}
	defer deferred()

	value, err := hostAttributeValue(nil, "db.example", "role")
	if err == nil {
		t.Fatal("hostAttributeValue() error = nil; want missing attribute error")
	}
	if value != "" {
		t.Fatalf("hostAttributeValue() = %q; want empty value", value)
	}
}

func TestHostAttributeValueReturnsFirstMatch(t *testing.T) {
	value, err := hostAttributeValue([]domain.HostAttributes{{AttributeValue: "primary"}}, "db.example", "role")
	if err != nil || value != "primary" {
		t.Fatalf("hostAttributeValue() = %q, %v; want primary, nil", value, err)
	}
}
