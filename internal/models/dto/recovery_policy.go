// Package dto contains input contracts accepted at transport boundaries.
package dto

import "github.com/openark/orchestrator/internal/models/domain"

type SaveRecoveryPolicyCommand struct {
	ScopeType        string                     `json:"scopeType"`
	ScopeKey         string                     `json:"scopeKey"`
	ExpectedRevision int64                      `json:"expectedRevision"`
	Overrides        domain.RecoveryPolicyPatch `json:"overrides"`
	UpdatedBy        string                     `json:"updatedBy"`
	ChangeReason     string                     `json:"changeReason"`
}

type SaveRecoveryHookProfileCommand struct {
	Profile          domain.RecoveryHookProfile `json:"profile"`
	ExpectedRevision int64                      `json:"expectedRevision"`
}

type SaveRecoveryHookAssignmentCommand struct {
	Assignment       domain.RecoveryHookAssignment `json:"assignment"`
	ExpectedRevision int64                         `json:"expectedRevision"`
}
