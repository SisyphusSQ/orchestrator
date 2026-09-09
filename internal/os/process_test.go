package os

import (
	"context"
	"fmt"
	stdos "os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandRun(t *testing.T) {
	cmdErr := CommandRun("echo \"VAR1=$VAR1 VAR2=$VAR2\" && exit 11", []string{"VAR1=a", "VAR2=b"})
	if cmdErr == nil {
		t.Error("Expected CommandRun to fail, but no error returned")
	}

	expectedMsg := "(exit status 11) VAR1=a VAR2=b\n"
	if cmdErr.Error() != expectedMsg {
		t.Errorf("Expected CommandRun to return an Error '%s' but got '%s'", expectedMsg, cmdErr.Error())
	}
}

func TestCommandRunContextKillsChildProcessGroupOnTimeout(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "unexpected-marker")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := CommandRunContext(ctx, fmt.Sprintf("(sleep 0.4; printf done > %q) & wait", marker), []string{}, 1024)
	if err == nil {
		t.Fatal("timed out command returned nil")
	}
	time.Sleep(500 * time.Millisecond)
	if _, statErr := stdos.Stat(marker); !stdos.IsNotExist(statErr) {
		t.Fatalf("child process survived cancellation: %v", statErr)
	}
}

func TestCommandRunContextBoundsOutputAndKeepsItOutOfErrors(t *testing.T) {
	output, err := CommandRunContext(context.Background(), "printf 'password=secret-123456'; exit 7", []string{}, 12)
	if err == nil {
		t.Fatal("CommandRunContext returned nil for a failed command")
	}
	if len(output) != 12 {
		t.Fatalf("bounded output = %q (length %d), want length 12", output, len(output))
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), output) {
		t.Fatalf("command output leaked into error: %v", err)
	}
}
