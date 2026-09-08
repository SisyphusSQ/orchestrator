package process

import (
	"errors"
	"strings"
	"testing"
)

func TestHostnameErrorReturnsResolutionFailure(t *testing.T) {
	previousErr := thisHostnameErr
	thisHostnameErr = errors.New("hostname unavailable")
	t.Cleanup(func() {
		thisHostnameErr = previousErr
	})

	err := HostnameError()
	if err == nil {
		t.Fatal("HostnameError() returned nil")
	}
	if !strings.Contains(err.Error(), "hostname unavailable") {
		t.Fatalf("HostnameError() = %q; want resolution failure", err)
	}
}
