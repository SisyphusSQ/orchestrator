// Package vo contains output contracts returned at transport boundaries.
package vo

import "github.com/openark/orchestrator/internal/models/domain"

type RecoveryPolicyDocument struct {
	ScopeType    string                     `json:"scopeType"`
	ScopeKey     string                     `json:"scopeKey"`
	Revision     int64                      `json:"revision"`
	Overrides    domain.RecoveryPolicyPatch `json:"overrides"`
	Inherited    domain.RecoveryPolicy      `json:"inherited"`
	Effective    domain.RecoveryPolicy      `json:"effective"`
	UpdatedBy    string                     `json:"updatedBy,omitempty"`
	ChangeReason string                     `json:"changeReason,omitempty"`
	UpdatedAt    string                     `json:"updatedAt,omitempty"`
}
