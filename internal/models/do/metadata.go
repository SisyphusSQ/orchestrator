// Package do contains stable persistence records used by repositories.
// These types describe database rows only; they are not HTTP contracts.
package do

import "database/sql"

// HostAttribute is the persisted representation of host_attributes.
type HostAttribute struct {
	Hostname        string `gorm:"column:hostname"`
	AttributeName   string `gorm:"column:attribute_name"`
	AttributeValue  string `gorm:"column:attribute_value"`
	SubmitTimestamp string `gorm:"column:submit_timestamp"`
	ExpireTimestamp string `gorm:"column:expire_timestamp"`
}

// KVStoreEntry is the persisted representation returned from kv_store.
type KVStoreEntry struct {
	Value string `gorm:"column:store_value"`
}

// AccessTokenSecret is the private portion of an access token row.
type AccessTokenSecret struct {
	SecretToken string `gorm:"column:secret_token"`
}

// AccessTokenValidity is an aggregate over matching access token rows.
type AccessTokenValidity struct {
	Count int `gorm:"column:valid_token"`
}

// RecoveryDisabled is an aggregate over the global recovery brake row.
type RecoveryDisabled struct {
	Count int `gorm:"column:disabled_count"`
}

// NodeHealth is the stable persistence record returned from node_health.
type NodeHealth struct {
	Hostname        string `gorm:"column:hostname"`
	Token           string `gorm:"column:token"`
	AppVersion      string `gorm:"column:app_version"`
	FirstSeenActive string `gorm:"column:first_seen_active"`
	LastSeenActive  string `gorm:"column:last_seen_active"`
	DBBackend       string `gorm:"column:db_backend"`
}

// HealthyToken is a token associated with a live HTTP node.
type HealthyToken struct {
	Token string `gorm:"column:token"`
}

// Agent is the persisted representation returned from host_agent.
type Agent struct {
	Hostname      string        `gorm:"column:hostname"`
	Port          int           `gorm:"column:port"`
	Token         string        `gorm:"column:token"`
	LastSubmitted string        `gorm:"column:last_submitted"`
	MySQLPort     sql.NullInt64 `gorm:"column:mysql_port"`
}

// Hostname is a single-hostname projection.
type Hostname struct {
	Hostname string `gorm:"column:hostname"`
}

// SeedOperation is the persisted representation returned from agent_seed.
type SeedOperation struct {
	SeedID         int64  `gorm:"column:id"`
	TargetHostname string `gorm:"column:target_hostname"`
	SourceHostname string `gorm:"column:source_hostname"`
	StartTimestamp string `gorm:"column:start_timestamp"`
	EndTimestamp   string `gorm:"column:end_timestamp"`
	IsComplete     bool   `gorm:"column:is_complete"`
	IsSuccessful   bool   `gorm:"column:is_successful"`
}

// SeedOperationState is the persisted representation returned from agent_seed_state.
type SeedOperationState struct {
	StateID        int64  `gorm:"column:id"`
	SeedID         int64  `gorm:"column:agent_seed_id"`
	StateTimestamp string `gorm:"column:state_timestamp"`
	Action         string `gorm:"column:state_action"`
	ErrorMessage   string `gorm:"column:error_message"`
}

// RecoveryPolicy is the persisted recovery policy document.
type RecoveryPolicy struct {
	ScopeType    string `gorm:"column:scope_type"`
	ScopeKey     string `gorm:"column:scope_key"`
	PolicyJSON   string `gorm:"column:policy_json"`
	Revision     int64  `gorm:"column:revision"`
	UpdatedBy    string `gorm:"column:updated_by"`
	ChangeReason string `gorm:"column:change_reason"`
	UpdatedAt    string `gorm:"column:updated_at"`
}

// ClusterAlias is a persisted explicit cluster alias.
type ClusterAlias struct {
	ClusterName string `gorm:"column:cluster_name"`
	Alias       string `gorm:"column:alias"`
}

// RowCount is a named aggregate count used by metadata repositories.
type RowCount struct {
	Count int `gorm:"column:row_count"`
}

// RecoveryHookProfile is the persisted hook profile record.
type RecoveryHookProfile struct {
	ID               string `gorm:"column:profile_id"`
	Name             string `gorm:"column:profile_name"`
	CommandsJSON     string `gorm:"column:commands_json"`
	TimeoutSeconds   int    `gorm:"column:timeout_seconds"`
	FailurePolicy    string `gorm:"column:failure_policy"`
	OutputLimitBytes int    `gorm:"column:output_limit_bytes"`
	Enabled          bool   `gorm:"column:enabled"`
	Revision         int64  `gorm:"column:revision"`
	UpdatedBy        string `gorm:"column:updated_by"`
	ChangeReason     string `gorm:"column:change_reason"`
	UpdatedAt        string `gorm:"column:updated_at"`
}

// RecoveryHookAssignment is the persisted hook assignment record.
type RecoveryHookAssignment struct {
	ScopeType      string `gorm:"column:scope_type"`
	ScopeKey       string `gorm:"column:scope_key"`
	Phase          string `gorm:"column:phase"`
	Mode           string `gorm:"column:mode"`
	ProfileIDsJSON string `gorm:"column:profile_ids_json"`
	Revision       int64  `gorm:"column:revision"`
	UpdatedBy      string `gorm:"column:updated_by"`
	ChangeReason   string `gorm:"column:change_reason"`
	UpdatedAt      string `gorm:"column:updated_at"`
}

// InstanceKey is a hostname/port persistence projection.
type InstanceKey struct {
	Hostname string `gorm:"column:hostname"`
	Port     int    `gorm:"column:port"`
}

// CandidateDatabaseInstance is the persisted failover candidate record.
type CandidateDatabaseInstance struct {
	Hostname            string `gorm:"column:hostname"`
	Port                int    `gorm:"column:port"`
	PromotionRule       string `gorm:"column:promotion_rule"`
	LastSuggested       string `gorm:"column:last_suggested"`
	PromotionRuleExpiry string `gorm:"column:promotion_rule_expiry"`
}

// EquivalentCoordinates is a projected master position equivalence.
type EquivalentCoordinates struct {
	Hostname   string `gorm:"column:hostname"`
	Port       int    `gorm:"column:port"`
	BinlogFile string `gorm:"column:binlog_file"`
	BinlogPos  int64  `gorm:"column:binlog_pos"`
}

// InstanceTag is a persisted instance tag.
type InstanceTag struct {
	Name  string `gorm:"column:tag_name"`
	Value string `gorm:"column:tag_value"`
}

// TagValue is a tag value projection.
type TagValue struct {
	Value string `gorm:"column:tag_value"`
}

// Maintenance is the persisted maintenance record projection.
type Maintenance struct {
	ID             uint   `gorm:"column:id"`
	Hostname       string `gorm:"column:hostname"`
	Port           int    `gorm:"column:port"`
	BeginTimestamp string `gorm:"column:begin_timestamp"`
	SecondsElapsed uint   `gorm:"column:seconds_elapsed"`
	Active         bool   `gorm:"column:maintenance_active"`
	Owner          string `gorm:"column:owner"`
	Reason         string `gorm:"column:reason"`
}

// MaintenanceState is an aggregate maintenance state projection.
type MaintenanceState struct {
	Active bool `gorm:"column:in_maintenance"`
}

// Downtime is the persisted downtime record projection.
type Downtime struct {
	Hostname       string `gorm:"column:hostname"`
	Port           int    `gorm:"column:port"`
	BeginTimestamp string `gorm:"column:begin_timestamp"`
	EndTimestamp   string `gorm:"column:end_timestamp"`
	Owner          string `gorm:"column:owner"`
	Reason         string `gorm:"column:reason"`
}

// ClusterPoolInstance is a cluster/pool/instance association projection.
type ClusterPoolInstance struct {
	ClusterName  string `gorm:"column:cluster_name"`
	ClusterAlias string `gorm:"column:alias"`
	Pool         string `gorm:"column:pool"`
	Hostname     string `gorm:"column:hostname"`
	Port         int    `gorm:"column:port"`
}

// PoolInstancesSubmission is an aggregate pool submission projection.
type PoolInstancesSubmission struct {
	Pool         string `gorm:"column:pool"`
	RegisteredAt string `gorm:"column:registered_at"`
	Hosts        string `gorm:"column:hosts"`
}

// Audit is the persisted audit record projection.
type Audit struct {
	ID        int64  `gorm:"column:id"`
	Timestamp string `gorm:"column:audit_timestamp"`
	Type      string `gorm:"column:audit_type"`
	Hostname  string `gorm:"column:hostname"`
	Port      int    `gorm:"column:port"`
	Message   string `gorm:"column:message"`
}

// HostnameResolve is a persisted hostname resolution projection.
type HostnameResolve struct {
	Hostname         string `gorm:"column:hostname"`
	ResolvedHostname string `gorm:"column:resolved_hostname"`
}

// HostnameUnresolve is a persisted reverse hostname resolution projection.
type HostnameUnresolve struct {
	Hostname           string `gorm:"column:hostname"`
	UnresolvedHostname string `gorm:"column:unresolved_hostname"`
}

// MissingHostnameResolve is an unresolved hostname/port projection.
type MissingHostnameResolve struct {
	UnresolvedHostname string `gorm:"column:unresolved_hostname"`
	Port               int    `gorm:"column:port"`
}

// HostnameIP is a persisted hostname IP projection.
type HostnameIP struct {
	IPv4 string `gorm:"column:ipv4"`
	IPv6 string `gorm:"column:ipv6"`
}

// ClusterName is a cluster-name projection.
type ClusterName struct {
	ClusterName string `gorm:"column:cluster_name"`
}

// SuggestedClusterAlias is an unambiguous suggested alias projection.
type SuggestedClusterAlias struct {
	SuggestedAlias string `gorm:"column:suggested_cluster_alias"`
	Hostname       string `gorm:"column:hostname"`
	Port           int    `gorm:"column:port"`
}
