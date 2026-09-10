package dto

// ReplicationAnalysisHints controls filtering and audit side effects for an
// analysis request.
type ReplicationAnalysisHints struct {
	IncludeDowntimed bool
	IncludeNoProblem bool
	AuditAnalysis    bool
}
