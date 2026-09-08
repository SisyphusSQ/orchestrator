package orcraft

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/raft"
)

// Class identifies a raft API error category that HTTP handlers can map
// without collapsing distinct failure modes.
type Class string

const (
	ClassInvalidArgument Class = "invalid_argument"
	ClassDisabled        Class = "raft_disabled"
	ClassNotBootstrapped Class = "not_bootstrapped"
	ClassNotLeader       Class = "not_leader"
	ClassConflict        Class = "conflict"
	ClassNotFound        Class = "not_found"
	ClassFailed          Class = "failed"
	ClassIndeterminate   Class = "indeterminate"
)

// Error is a typed raft adapter error.
type Error struct {
	Class   Class
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	if !ok || e == nil || other == nil {
		return false
	}
	if other.Class != "" && e.Class != other.Class {
		return false
	}
	if other.Message != "" && e.Message != other.Message {
		return false
	}
	return true
}

func newError(class Class, message string) *Error {
	return &Error{Class: class, Message: message}
}

func wrapError(class Class, message string, err error) *Error {
	return &Error{Class: class, Message: message, Err: err}
}

var (
	ErrNotEnabled              = newError(ClassDisabled, "raft is not configured/running")
	ErrNotLeader               = newError(ClassNotLeader, "not leader")
	ErrNotBootstrapped         = newError(ClassNotBootstrapped, "raft cluster is not bootstrapped")
	ErrAlreadyBootstrapped     = newError(ClassConflict, "raft cluster already has state or configuration")
	ErrStaleConfiguration      = newError(ClassConflict, "configuration index conflict")
	ErrIdentityConflict        = newError(ClassConflict, "server id and address conflict with existing configuration")
	ErrNotFound                = newError(ClassNotFound, "server id not found in configuration")
	ErrConfigurationInProgress = newError(ClassIndeterminate, "raft configuration change is not committed")
	ErrIndeterminate           = newError(ClassIndeterminate, "raft mutation result is indeterminate")
	ErrInvalidArgument         = newError(ClassInvalidArgument, "invalid raft request")
	ErrTimeout                 = newError(ClassIndeterminate, "raft future timed out")

	// RaftNotRunning is the historical alias used by existing call sites.
	RaftNotRunning = ErrNotEnabled
)

func invalidArgument(format string, args ...interface{}) *Error {
	return newError(ClassInvalidArgument, fmt.Sprintf(format, args...))
}

func classifyRaftError(err error) error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	switch {
	case errors.Is(err, raft.ErrNotLeader):
		return wrapError(ClassNotLeader, "not leader", err)
	case errors.Is(err, raft.ErrLeadershipLost), errors.Is(err, ErrTimeout):
		return wrapError(ClassIndeterminate, "raft mutation result is indeterminate", err)
	case errors.Is(err, raft.ErrLeadershipTransferInProgress):
		return wrapError(ClassConflict, "raft leadership transfer is already in progress", err)
	case errors.Is(err, raft.ErrCantBootstrap):
		return ErrAlreadyBootstrapped
	case errors.Is(err, raft.ErrEnqueueTimeout):
		return wrapError(ClassFailed, "raft operation was not enqueued", err)
	case errors.Is(err, raft.ErrRaftShutdown):
		return wrapError(ClassFailed, "raft is shutdown", err)
	case strings.Contains(err.Error(), "configuration changed since"):
		return wrapError(ClassConflict, "configuration index conflict", err)
	default:
		return wrapError(ClassFailed, "raft operation failed", err)
	}
}

func classifyLeadershipTransferError(err error) error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		return typed
	}
	switch {
	case errors.Is(err, raft.ErrNotLeader),
		errors.Is(err, raft.ErrLeadershipLost),
		errors.Is(err, raft.ErrLeadershipTransferInProgress),
		errors.Is(err, raft.ErrEnqueueTimeout),
		errors.Is(err, raft.ErrRaftShutdown):
		return classifyRaftError(err)
	default:
		// After a leadership-transfer request is enqueued, an untyped runtime
		// error can arrive after TimeoutNow reached the target. The caller cannot
		// prove whether a new leader will still emerge, so this is not a
		// confirmed failure.
		return wrapError(ClassIndeterminate, "raft leadership transfer result is indeterminate", err)
	}
}

// ClassOf returns the error class for an adapter or official raft error.
func ClassOf(err error) Class {
	if err == nil {
		return ""
	}
	var typed *Error
	if errors.As(err, &typed) {
		return typed.Class
	}
	classified := classifyRaftError(err)
	if errors.As(classified, &typed) {
		return typed.Class
	}
	return ClassFailed
}
