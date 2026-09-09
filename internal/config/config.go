/*
   Copyright 2014 Outbrain Inc.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"gopkg.in/gcfg.v1"

	"github.com/openark/orchestrator/internal/golib/log"
)

var (
	envVariableRegexp = regexp.MustCompile("[$][{](.*)[}]")
)

const (
	LostInRecoveryDowntimeSeconds int = 60 * 60 * 24 * 365
	DefaultStatusAPIEndpoint          = "/api/status"
)

var raftConfigurationLocked bool

// LockRaftConfiguration freezes node identity and transport settings for the running process.
func LockRaftConfiguration() { raftConfigurationLocked = true }

var configurationLoaded chan bool = make(chan bool)

const (
	HealthPollSeconds                            = 1
	RaftHealthPollSeconds                        = 10
	RecoveryPollSeconds                          = 1
	BinlogFileHistoryDays                        = 1
	MaintenanceOwner                             = "orchestrator"
	AuditPageSize                                = 20
	MaintenancePurgeDays                         = 7
	MySQLTopologyMaxPoolConnections              = 3
	MaintenanceExpireMinutes                     = 10
	AgentHttpTimeoutSeconds                      = 60
	PseudoGTIDCoordinatesHistoryHeuristicMinutes = 2
	PseudoGTIDSchema                             = "_pseudo_gtid_"
	PseudoGTIDIntervalSeconds                    = 5
	PseudoGTIDExpireMinutes                      = 60
	StaleInstanceCoordinatesExpireSeconds        = 60
	CheckAutoPseudoGTIDGrantsIntervalSeconds     = 60
	SelectTrueQuery                              = "select 1"
	ConsulKVsPerCluster                          = 5 // KVs: "/", "/hostname", "/ipv4", "/ipv6" and "/port"
	ConsulMaxTransactionOps                      = 64
)

// Configuration makes for orchestrator configuration input, which can be provided by user via JSON or YAML.
// Some of the parameteres have reasonable default values, and some (like database credentials) are
// strictly expected from user.
type Configuration struct {
	OTelTraceEndpoint    string  // Full OTLP HTTP /v1/traces URL; empty disables trace export.
	OTelTraceSampleRatio float64 // Parent-based root sampling ratio in [0,1]. Restart required.

	Debug                                     bool   // set debug mode (similar to --debug option)
	EnableSyslog                              bool   // Direct logs to syslog in addition to stderr; initialization failure stops startup.
	ListenAddress                             string // Where orchestrator HTTP should listen for TCP
	ListenSocket                              string // Where orchestrator HTTP should listen for unix socket (default: empty; when given, TCP is disabled)
	HTTPAdvertise                             string // optional, for raft setups, what is the HTTP address this node will advertise to its peers (potentially use where behind NAT or when rerouting ports; example: "http://11.22.33.44:3030")
	AgentsServerPort                          string // port orchestrator agents talk back to
	MySQLTopologyUser                         string
	MySQLTopologyPassword                     string
	MySQLTopologyCredentialsConfigFile        string // my.cnf style configuration file from where to pick credentials. Expecting `user`, `password` under `[client]` section
	MySQLTopologySSLPrivateKeyFile            string // Private key file used to authenticate with a Topology mysql instance with TLS
	MySQLTopologySSLCertFile                  string // Certificate PEM file used to authenticate with a Topology mysql instance with TLS
	MySQLTopologySSLCAFile                    string // Certificate Authority PEM file used to authenticate with a Topology mysql instance with TLS
	MySQLTopologySSLSkipVerify                bool   // If true, do not strictly validate mutual TLS certs for Topology mysql instances
	MySQLTopologyUseMutualTLS                 bool   // Turn on TLS authentication with the Topology MySQL instances
	MySQLTopologyUseMixedTLS                  bool   // Mixed TLS and non-TLS authentication with the Topology MySQL instances
	MySQLTopologyMaxAllowedPacket             int32  // max_allowed_packet value to use when connecting to the Topology mysql instance
	TLSCacheTTLFactor                         uint   // Factor of InstancePollSeconds that we set as TLS info cache expiry
	BackendDB                                 string // EXPERIMENTAL: type of backend db; either "mysql" or "sqlite3"
	SQLite3DataFile                           string // when BackendDB == "sqlite3", full path to sqlite3 datafile
	SkipOrchestratorDatabaseUpdate            bool   // When true, do not check backend database schema nor attempt to update it. Useful when you may be running multiple versions of orchestrator, and you only wish certain boxes to dictate the db structure (or else any time a different orchestrator version runs it will rebuild database schema)
	PanicIfDifferentDatabaseDeploy            bool   // When true, and this process finds the orchestrator backend DB was provisioned by a different version, panic
	RaftNodeID                                string // Stable raft server ID. Required for server startup. Never derived from bind/advertise/DNS.
	RaftBind                                  string // Local raft listen address (host:port)
	RaftAdvertise                             string // Cluster-facing raft address (host:port). Defaults to normalized RaftBind.
	RaftDataDir                               string
	DefaultRaftPort                           int // if a raft bind/advertise address does not specify port, use this one
	MySQLOrchestratorHost                     string
	MySQLOrchestratorMaxPoolConnections       int // The maximum size of the connection pool to the Orchestrator backend.
	MySQLOrchestratorPort                     uint
	MySQLOrchestratorDatabase                 string
	MySQLOrchestratorUser                     string
	MySQLOrchestratorPassword                 string
	MySQLOrchestratorCredentialsConfigFile    string   // my.cnf style configuration file from where to pick credentials. Expecting `user`, `password` under `[client]` section
	MySQLOrchestratorSSLPrivateKeyFile        string   // Private key file used to authenticate with the Orchestrator mysql instance with TLS
	MySQLOrchestratorSSLCertFile              string   // Certificate PEM file used to authenticate with the Orchestrator mysql instance with TLS
	MySQLOrchestratorSSLCAFile                string   // Certificate Authority PEM file used to authenticate with the Orchestrator mysql instance with TLS
	MySQLOrchestratorSSLSkipVerify            bool     // If true, do not strictly validate mutual TLS certs for the Orchestrator mysql instances
	MySQLOrchestratorUseMutualTLS             bool     // Turn on TLS authentication with the Orchestrator MySQL instance
	MySQLOrchestratorReadTimeoutSeconds       int      // Number of seconds before backend mysql read operation is aborted (driver-side)
	MySQLOrchestratorRejectReadOnly           bool     // Reject read only connections https://github.com/go-sql-driver/mysql#rejectreadonly
	MySQLOrchestratorMaxAllowedPacket         int32    // max_allowed_packet to use when connecting to the Orchestrator mysql instance
	MySQLConnectTimeoutSeconds                int      // Number of seconds before connection is aborted (driver-side)
	MySQLDiscoveryReadTimeoutSeconds          int      // Number of seconds before topology mysql read operation is aborted (driver-side). Used for discovery queries.
	MySQLTopologyReadTimeoutSeconds           int      // Number of seconds before topology mysql read operation is aborted (driver-side). Used for all but discovery queries.
	MySQLConnectionLifetimeSeconds            int      // Number of seconds the mysql driver will keep database connection alive before recycling it
	DefaultInstancePort                       int      // In case port was not specified on command line
	ReplicationLagQuery                       string   // custom query to check on replica lg (e.g. heartbeat table). Must return a single row with a single numeric column, which is the lag.
	ReplicationCredentialsQuery               string   // custom query to get replication credentials. Must return a single row, with five text columns: 1st is username, 2nd is password, 3rd is SSLCaCert, 4th is SSLCert, 5th is SSLKey. This is optional, and can be used by orchestrator to configure replication after master takeover or setup of co-masters. You need to ensure the orchestrator user has the privileges to run this query
	DiscoverByShowSlaveHosts                  bool     // Attempt SHOW SLAVE HOSTS before PROCESSLIST
	UseSuperReadOnly                          bool     // Should orchestrator super_read_only any time it sets read_only
	InstancePollSeconds                       uint     // Number of seconds between instance reads
	DeadInstancePollSecondsMultiplyFactor     float32  // InstancePoolSeconds increase factor for dead instances read time calculation
	DeadInstancePollSecondsMax                uint     // Maximum delay between dead instance read attempts
	DeadInstanceDiscoveryMaxConcurrency       uint     // Number of goroutines doing dead hosts discovery
	DeadInstanceDiscoveryLogsEnabled          bool     // Enable logs related to dead instances discoveries
	ReasonableInstanceCheckSeconds            uint     // Number of seconds an instance read is allowed to take before it is considered invalid, i.e. before LastCheckValid will be false
	InstanceWriteBufferSize                   int      // Instance write buffer size (max number of instances to flush in one INSERT ODKU)
	BufferInstanceWrites                      bool     // Set to 'true' for write-optimization on backend table (compromise: writes can be stale and overwrite non stale data)
	InstanceFlushIntervalMilliseconds         int      // Max interval between instance write buffer flushes
	SkipMaxScaleCheck                         bool     // If you don't ever have MaxScale BinlogServer in your topology (and most people don't), set this to 'true' to save some pointless queries
	LowerReplicaVersionAllowed                bool     // Allow lower version replica to replicate from higher version of the replication source. If that's the case - warning is produced.
	UnseenInstanceForgetHours                 uint     // Number of hours after which an unseen instance is forgotten
	SnapshotTopologiesIntervalHours           uint     // Interval in hour between snapshot-topologies invocation. Default: 0 (disabled)
	DiscoveryMaxConcurrency                   uint     // Number of goroutines doing hosts discovery
	DiscoveryQueueCapacity                    uint     // Buffer size of the discovery queue. Should be greater than the number of DB instances being discovered
	DiscoverySeeds                            []string // Hard coded array of hostname:port, ensuring orchestrator discovers these hosts upon startup, assuming not already known to orchestrator
	InstanceBulkOperationsWaitTimeoutSeconds  uint     // Time to wait on a single instance when doing bulk (many instances) operation
	HostnameResolveMethod                     string   // Method by which to "normalize" hostname ("none"/"default"/"cname")
	MySQLHostnameResolveMethod                string   // Method by which to "normalize" hostname via MySQL server. ("none"/"@@hostname"/"@@report_host"; default "@@hostname")
	SkipBinlogServerUnresolveCheck            bool     // Skip the double-check that an unresolved hostname resolves back to same hostname for binlog servers
	ExpiryHostnameResolvesMinutes             int      // Number of minutes after which to expire hostname-resolves
	RejectHostnameResolvePattern              string   // Regexp pattern for resolved hostname that will not be accepted (not cached, not written to db). This is done to avoid storing wrong resolves due to network glitches.
	CandidateInstanceExpireMinutes            uint     // Minutes after which a suggestion to use an instance as a candidate replica (to be preferably promoted on master failover) is expired.
	AuditLogFile                              string   // Name of log file for audit operations. Disabled when empty.
	AuditToSyslog                             bool     // Write audit messages to syslog; initialization failure stops startup.
	AuditToBackendDB                          bool     // If true, audit messages are written to the backend DB's `audit` table (default: true)
	AuditPurgeDays                            uint     // Days after which audit entries are purged from the database
	RemoveTextFromHostnameDisplay             string   // Text to strip off the hostname on cluster/clusters pages
	ReadOnly                                  bool
	AuthenticationMethod                      string            // Type of autherntication to use, if any. "" for none, "basic" for BasicAuth, "multi" for advanced BasicAuth, "proxy" for forwarded credentials via reverse proxy, "token" for token based access
	HTTPAuthUser                              string            // Username for HTTP Basic authentication (blank disables authentication)
	HTTPAuthPassword                          string            // Password for HTTP Basic authentication
	AuthUserHeader                            string            // HTTP header indicating auth user, when AuthenticationMethod is "proxy"
	PowerAuthUsers                            []string          // On AuthenticationMethod == "proxy", list of users that can make changes. All others are read-only.
	PowerAuthGroups                           []string          // list of unix groups the authenticated user must be a member of to make changes.
	ConfigurationAdminUsers                   []string          // Authenticated users allowed to change recovery policies and hooks. Empty denies configuration writes when authentication is enabled.
	ConfigurationAdminGroups                  []string          // Unix groups allowed to change recovery policies and hooks.
	AccessTokenUseExpirySeconds               uint              // Time by which an issued token must be used
	AccessTokenExpiryMinutes                  uint              // Time after which HTTP access token expires
	ClusterNameToAlias                        map[string]string // map between regex matching cluster name to a human friendly alias
	DetectClusterAliasQuery                   string            // Optional query (executed on topology instance) that returns the alias of a cluster. Query will only be executed on cluster master (though until the topology's master is resovled it may execute on other/all replicas). If provided, must return one row, one column
	DetectClusterDomainQuery                  string            // Optional query (executed on topology instance) that returns the VIP/CNAME/Alias/whatever domain name for the master of this cluster. Query will only be executed on cluster master (though until the topology's master is resovled it may execute on other/all replicas). If provided, must return one row, one column
	DetectInstanceAliasQuery                  string            // Optional query (executed on topology instance) that returns the alias of an instance. If provided, must return one row, one column
	DetectPromotionRuleQuery                  string            // Optional query (executed on topology instance) that returns the promotion rule of an instance. If provided, must return one row, one column.
	DataCenterPattern                         string            // Regexp pattern with one group, extracting the datacenter name from the hostname
	RegionPattern                             string            // Regexp pattern with one group, extracting the region name from the hostname
	PhysicalEnvironmentPattern                string            // Regexp pattern with one group, extracting physical environment info from hostname (e.g. combination of datacenter & prod/dev env)
	DetectDataCenterQuery                     string            // Optional query (executed on topology instance) that returns the data center of an instance. If provided, must return one row, one column. Overrides DataCenterPattern and useful for installments where DC cannot be inferred by hostname
	DetectRegionQuery                         string            // Optional query (executed on topology instance) that returns the region of an instance. If provided, must return one row, one column. Overrides RegionPattern and useful for installments where Region cannot be inferred by hostname
	DetectPhysicalEnvironmentQuery            string            // Optional query (executed on topology instance) that returns the physical environment of an instance. If provided, must return one row, one column. Overrides PhysicalEnvironmentPattern and useful for installments where env cannot be inferred by hostname
	DetectSemiSyncEnforcedQuery               string            // Optional query (executed on topology instance) to determine whether semi-sync is fully enforced for master writes (async fallback is not allowed under any circumstance). If provided, must return one row, one column, value 0 or 1.
	SupportFuzzyPoolHostnames                 bool              // Should "submit-pool-instances" command be able to pass list of fuzzy instances (fuzzy means non-fqdn, but unique enough to recognize). Defaults 'true', implies more queries on backend db
	InstancePoolExpiryMinutes                 uint              // Time after which entries in database_instance_pool are expired (resubmit via `submit-pool-instances`)
	ServeAgentsHttp                           bool              // Spawn another HTTP interface dedicated for orchestrator-agent
	AgentsUseSSL                              bool              // When "true" orchestrator will listen on agents port with SSL as well as connect to agents via SSL
	AgentsUseMutualTLS                        bool              // When "true" Use mutual TLS for the server to agent communication
	AgentSSLSkipVerify                        bool              // When using SSL for the Agent, should we ignore SSL certification error
	AgentSSLPrivateKeyFile                    string            // Name of Agent SSL private key file, applies only when AgentsUseSSL = true
	AgentSSLCertFile                          string            // Name of Agent SSL certification file, applies only when AgentsUseSSL = true
	AgentSSLCAFile                            string            // Name of the Agent Certificate Authority file, applies only when AgentsUseSSL = true
	AgentSSLValidOUs                          []string          // Valid organizational units when using mutual TLS to communicate with the agents
	UseSSL                                    bool              // Use SSL on the server web port
	UseMutualTLS                              bool              // When "true" Use mutual TLS for the server's web and API connections
	SSLSkipVerify                             bool              // When using SSL, should we ignore SSL certification error
	SSLPrivateKeyFile                         string            // Name of SSL private key file, applies only when UseSSL = true
	SSLCertFile                               string            // Name of SSL certification file, applies only when UseSSL = true
	SSLCAFile                                 string            // Name of the Certificate Authority file, applies only when UseSSL = true
	SSLValidOUs                               []string          // Valid organizational units when using mutual TLS
	StatusEndpoint                            string            // Override the status endpoint.  Defaults to '/api/status'
	StatusOUVerify                            bool              // If true, try to verify OUs when Mutual TLS is on.  Defaults to false
	AgentPollMinutes                          uint              // Minutes between agent polling
	UnseenAgentForgetHours                    uint              // Number of hours after which an unseen agent is forgotten
	StaleSeedFailMinutes                      uint              // Number of minutes after which a stale (no progress) seed is considered failed.
	SeedWaitSecondsBeforeSend                 int64             // Number of seconds for waiting before start send data command on agent
	AutoPseudoGTID                            bool              // Should orchestrator automatically inject Pseudo-GTID entries to the masters
	PseudoGTIDPattern                         string            // Pattern to look for in binary logs that makes for a unique entry (pseudo GTID). When empty, Pseudo-GTID based refactoring is disabled.
	PseudoGTIDPatternIsFixedSubstring         bool              // If true, then PseudoGTIDPattern is not treated as regular expression but as fixed substring, and can boost search time
	PseudoGTIDMonotonicHint                   string            // substring in Pseudo-GTID entry which indicates Pseudo-GTID entries are expected to be monotonically increasing
	DetectPseudoGTIDQuery                     string            // Optional query which is used to authoritatively decide whether pseudo gtid is enabled on instance
	BinlogEventsChunkSize                     int               // Chunk size (X) for SHOW BINLOG|RELAYLOG EVENTS LIMIT ?,X statements. Smaller means less locking and more work to be done
	SkipBinlogEventsContaining                []string          // When scanning/comparing binlogs for Pseudo-GTID, skip entries containing given texts. These are NOT regular expressions (would consume too much CPU while scanning binlogs), just substrings to find.
	ReduceReplicationAnalysisCount            bool              // When true, replication analysis will only report instances where possibility of handled problems is possible in the first place (e.g. will not report most leaf nodes, that are mostly uninteresting). When false, provides an entry for every known instance
	ProcessesShellCommand                     string            // Shell that executes command scripts
	OSCIgnoreHostnameFilters                  []string          // OSC replicas recommendation will ignore replica hostnames matching given patterns
	URLPrefix                                 string            // URL prefix to run orchestrator on non-root web path, e.g. /orchestrator to put it behind nginx.
	DiscoveryIgnoreReplicaHostnameFilters     []string          // Regexp filters to apply to prevent auto-discovering new replicas. Usage: unreachable servers due to firewalls, applications which trigger binlog dumps
	DiscoveryIgnoreMasterHostnameFilters      []string          // Regexp filters to apply to prevent auto-discovering a master. Usage: pointing your master temporarily to replicate some data from external host
	DiscoveryIgnoreHostnameFilters            []string          // Regexp filters to apply to prevent discovering instances of any kind
	DiscoveryIgnoreReplicationUsernameFilters []string          // Regexp filters to apply to prevent discovering instances that use a matching replication username
	EnableDiscoveryFiltersLogs                bool              // Should Orchestrator log the fact of filtered instance during discovery
	ConsulAddress                             string            // Address where Consul HTTP api is found. Example: 127.0.0.1:8500 or https://127.0.0.1:8501
	ConsulScheme                              string            // Scheme (http or https) for Consul; ignored when ConsulAddress includes a scheme
	ConsulAclToken                            string            // ACL token used to write to Consul KV; sent as X-Consul-Token
	ConsulDatacenter                          string            // Optional Consul datacenter passed as the default dc query parameter
	ConsulTLSCAFile                           string            // Optional CA file used to verify Consul HTTPS
	ConsulTLSCAPath                           string            // Optional CA directory used to verify Consul HTTPS
	ConsulTLSCertFile                         string            // Optional client certificate for Consul mTLS; requires ConsulTLSPrivateKeyFile
	ConsulTLSPrivateKeyFile                   string            // Optional client private key for Consul mTLS; requires ConsulTLSCertFile
	ConsulTLSServerName                       string            // Optional TLS ServerName / SNI when talking to Consul over HTTPS
	ConsulTLSSkipVerify                       bool              // If true, skip Consul TLS verification; default false. Must be explicit.
	ConsulHttpTimeoutSeconds                  int               // Overall Consul HTTP client timeout in seconds; 0 disables the deadline
	ConsulCrossDataCenterDistribution         bool              // should orchestrator automatically auto-deduce all consul DCs and write KVs in all DCs
	ConsulKVStoreProvider                     string            // Consul KV store provider (consul or consul-txn), default: "consul"
	ConsulMaxKVsPerTransaction                int               // Maximum number of KV operations to perform in a single Consul Transaction. Requires the "consul-txn" ConsulKVStoreProvider
	KVClusterMasterPrefix                     string            // Prefix to use for clusters' masters entries in KV stores (internal and Consul), default: "mysql/master"
	WebMessage                                string            // If provided, will be shown on all web pages below the title bar
	MaxConcurrentReplicaOperations            int               // Maximum number of concurrent operations on replicas
	PrependMessagesWithOrcIdentity            string            // use FQDN/hostname/custom to prefix error message returned to the client. Empty string (default)/none skips prefixing.
	CustomOrcIdentity                         string            // use if PrependMessagesWithOrcIdentity is 'custom'
}

// ToJSONString will marshal this configuration as JSON
func (this *Configuration) ToJSONString() string {
	b, _ := json.Marshal(this)
	return string(b)
}

// Config is *the* configuration instance, used globally to get configuration data
var Config = newConfiguration()
var readFileNames []string

func newConfiguration() *Configuration {
	return &Configuration{
		OTelTraceSampleRatio:                      0.1,
		Debug:                                     false,
		EnableSyslog:                              false,
		ListenAddress:                             ":3000",
		ListenSocket:                              "",
		HTTPAdvertise:                             "",
		AgentsServerPort:                          ":3001",
		StatusEndpoint:                            DefaultStatusAPIEndpoint,
		StatusOUVerify:                            false,
		BackendDB:                                 "mysql",
		SQLite3DataFile:                           "",
		SkipOrchestratorDatabaseUpdate:            false,
		PanicIfDifferentDatabaseDeploy:            false,
		RaftNodeID:                                "",
		RaftBind:                                  "127.0.0.1:10008",
		RaftAdvertise:                             "",
		RaftDataDir:                               "",
		DefaultRaftPort:                           10008,
		MySQLOrchestratorMaxPoolConnections:       128, // limit concurrent conns to backend DB
		MySQLOrchestratorPort:                     3306,
		MySQLTopologyUseMutualTLS:                 false,
		MySQLTopologyUseMixedTLS:                  true,
		MySQLTopologyMaxAllowedPacket:             -1,
		MySQLOrchestratorUseMutualTLS:             false,
		MySQLConnectTimeoutSeconds:                2,
		MySQLOrchestratorReadTimeoutSeconds:       30,
		MySQLOrchestratorRejectReadOnly:           false,
		MySQLOrchestratorMaxAllowedPacket:         -1,
		MySQLDiscoveryReadTimeoutSeconds:          10,
		MySQLTopologyReadTimeoutSeconds:           600,
		MySQLConnectionLifetimeSeconds:            0,
		DefaultInstancePort:                       3306,
		TLSCacheTTLFactor:                         100,
		InstancePollSeconds:                       5,
		DeadInstancePollSecondsMultiplyFactor:     1,
		DeadInstancePollSecondsMax:                5 * 60,
		DeadInstanceDiscoveryMaxConcurrency:       0,
		DeadInstanceDiscoveryLogsEnabled:          false,
		ReasonableInstanceCheckSeconds:            1,
		InstanceWriteBufferSize:                   100,
		BufferInstanceWrites:                      false,
		InstanceFlushIntervalMilliseconds:         100,
		SkipMaxScaleCheck:                         true,
		LowerReplicaVersionAllowed:                false,
		UnseenInstanceForgetHours:                 240,
		SnapshotTopologiesIntervalHours:           0,
		DiscoverByShowSlaveHosts:                  false,
		UseSuperReadOnly:                          false,
		DiscoveryMaxConcurrency:                   300,
		DiscoveryQueueCapacity:                    100000,
		DiscoverySeeds:                            []string{},
		InstanceBulkOperationsWaitTimeoutSeconds:  10,
		HostnameResolveMethod:                     "default",
		MySQLHostnameResolveMethod:                "@@hostname",
		SkipBinlogServerUnresolveCheck:            true,
		ExpiryHostnameResolvesMinutes:             60,
		RejectHostnameResolvePattern:              "",
		CandidateInstanceExpireMinutes:            60,
		AuditLogFile:                              "",
		AuditToSyslog:                             false,
		AuditToBackendDB:                          false,
		AuditPurgeDays:                            7,
		RemoveTextFromHostnameDisplay:             "",
		ReadOnly:                                  false,
		AuthenticationMethod:                      "",
		HTTPAuthUser:                              "",
		HTTPAuthPassword:                          "",
		AuthUserHeader:                            "X-Forwarded-User",
		PowerAuthUsers:                            []string{"*"},
		PowerAuthGroups:                           []string{},
		ConfigurationAdminUsers:                   []string{},
		ConfigurationAdminGroups:                  []string{},
		AccessTokenUseExpirySeconds:               60,
		AccessTokenExpiryMinutes:                  1440,
		ClusterNameToAlias:                        make(map[string]string),
		DetectClusterAliasQuery:                   "",
		DetectClusterDomainQuery:                  "",
		DetectInstanceAliasQuery:                  "",
		DetectPromotionRuleQuery:                  "",
		DataCenterPattern:                         "",
		PhysicalEnvironmentPattern:                "",
		DetectDataCenterQuery:                     "",
		DetectPhysicalEnvironmentQuery:            "",
		DetectSemiSyncEnforcedQuery:               "",
		SupportFuzzyPoolHostnames:                 true,
		InstancePoolExpiryMinutes:                 60,
		ServeAgentsHttp:                           false,
		AgentsUseSSL:                              false,
		AgentsUseMutualTLS:                        false,
		AgentSSLValidOUs:                          []string{},
		AgentSSLSkipVerify:                        false,
		AgentSSLPrivateKeyFile:                    "",
		AgentSSLCertFile:                          "",
		AgentSSLCAFile:                            "",
		UseSSL:                                    false,
		UseMutualTLS:                              false,
		SSLValidOUs:                               []string{},
		SSLSkipVerify:                             false,
		SSLPrivateKeyFile:                         "",
		SSLCertFile:                               "",
		SSLCAFile:                                 "",
		AgentPollMinutes:                          60,
		UnseenAgentForgetHours:                    6,
		StaleSeedFailMinutes:                      60,
		SeedWaitSecondsBeforeSend:                 2,
		AutoPseudoGTID:                            false,
		PseudoGTIDPattern:                         "",
		PseudoGTIDPatternIsFixedSubstring:         false,
		PseudoGTIDMonotonicHint:                   "",
		DetectPseudoGTIDQuery:                     "",
		BinlogEventsChunkSize:                     10000,
		SkipBinlogEventsContaining:                []string{},
		ReduceReplicationAnalysisCount:            true,
		ProcessesShellCommand:                     "bash",
		OSCIgnoreHostnameFilters:                  []string{},
		URLPrefix:                                 "",
		DiscoveryIgnoreReplicaHostnameFilters:     []string{},
		DiscoveryIgnoreReplicationUsernameFilters: []string{},
		EnableDiscoveryFiltersLogs:                true,
		ConsulAddress:                             "",
		ConsulScheme:                              "http",
		ConsulAclToken:                            "",
		ConsulDatacenter:                          "",
		ConsulTLSCAFile:                           "",
		ConsulTLSCAPath:                           "",
		ConsulTLSCertFile:                         "",
		ConsulTLSPrivateKeyFile:                   "",
		ConsulTLSServerName:                       "",
		ConsulTLSSkipVerify:                       false,
		ConsulHttpTimeoutSeconds:                  60,
		ConsulCrossDataCenterDistribution:         false,
		ConsulKVStoreProvider:                     "consul",
		ConsulMaxKVsPerTransaction:                ConsulKVsPerCluster,
		KVClusterMasterPrefix:                     "mysql/master",
		WebMessage:                                "",
		MaxConcurrentReplicaOperations:            5,
		PrependMessagesWithOrcIdentity:            "",
		CustomOrcIdentity:                         "",
	}
}

func (this *Configuration) postReadAdjustments() error {
	if err := this.validateTelemetry(); err != nil {
		return err
	}
	switch strings.ToLower(this.AuthenticationMethod) {
	case "", "basic", "multi", "proxy", "token":
	default:
		return fmt.Errorf("unsupported AuthenticationMethod %q", this.AuthenticationMethod)
	}
	if this.MySQLOrchestratorCredentialsConfigFile != "" {
		mySQLConfig := struct {
			Client struct {
				User     string
				Password string
			}
		}{}
		err := gcfg.ReadFileInto(&mySQLConfig, this.MySQLOrchestratorCredentialsConfigFile)
		if err != nil {
			return fmt.Errorf("parse orchestrator credentials file %s: %w", this.MySQLOrchestratorCredentialsConfigFile, err)
		}
		log.Debugf("Parsed orchestrator credentials from %s", this.MySQLOrchestratorCredentialsConfigFile)
		this.MySQLOrchestratorUser = mySQLConfig.Client.User
		this.MySQLOrchestratorPassword = mySQLConfig.Client.Password
	}
	{
		// We accept password in the form "${SOME_ENV_VARIABLE}" in which case we pull
		// the given variable from os env
		submatch := envVariableRegexp.FindStringSubmatch(this.MySQLOrchestratorPassword)
		if len(submatch) > 1 {
			this.MySQLOrchestratorPassword = os.Getenv(submatch[1])
		}
	}
	if this.MySQLTopologyCredentialsConfigFile != "" {
		mySQLConfig := struct {
			Client struct {
				User     string
				Password string
			}
		}{}
		err := gcfg.ReadFileInto(&mySQLConfig, this.MySQLTopologyCredentialsConfigFile)
		if err != nil {
			return fmt.Errorf("parse topology credentials file %s: %w", this.MySQLTopologyCredentialsConfigFile, err)
		}
		log.Debugf("Parsed topology credentials from %s", this.MySQLTopologyCredentialsConfigFile)
		this.MySQLTopologyUser = mySQLConfig.Client.User
		this.MySQLTopologyPassword = mySQLConfig.Client.Password
	}
	{
		// We accept password in the form "${SOME_ENV_VARIABLE}" in which case we pull
		// the given variable from os env
		submatch := envVariableRegexp.FindStringSubmatch(this.MySQLTopologyPassword)
		if len(submatch) > 1 {
			this.MySQLTopologyPassword = os.Getenv(submatch[1])
		}
	}

	if this.URLPrefix != "" {
		// Ensure the prefix starts with "/" and has no trailing one.
		this.URLPrefix = strings.TrimLeft(this.URLPrefix, "/")
		this.URLPrefix = strings.TrimRight(this.URLPrefix, "/")
		this.URLPrefix = "/" + this.URLPrefix
	}

	if this.IsSQLite() && this.SQLite3DataFile == "" {
		return fmt.Errorf("SQLite3DataFile must be set when BackendDB is sqlite3")
	}
	if this.IsSQLite() {
		//		this.HostnameResolveMethod = "none"
	}
	if this.KVClusterMasterPrefix != "/" {
		// "/" remains "/"
		// "prefix" turns to "prefix/"
		// "some/prefix///" turns to "some/prefix/"
		this.KVClusterMasterPrefix = strings.TrimRight(this.KVClusterMasterPrefix, "/")
		this.KVClusterMasterPrefix = fmt.Sprintf("%s/", this.KVClusterMasterPrefix)
	}
	if this.AutoPseudoGTID {
		this.PseudoGTIDPattern = "drop view if exists `_pseudo_gtid_`"
		this.PseudoGTIDPatternIsFixedSubstring = true
		this.PseudoGTIDMonotonicHint = "asc:"
		this.DetectPseudoGTIDQuery = SelectTrueQuery
	}
	if this.HTTPAdvertise != "" {
		u, err := url.Parse(this.HTTPAdvertise)
		if err != nil {
			return fmt.Errorf("Failed parsing HTTPAdvertise %s: %s", this.HTTPAdvertise, err.Error())
		}
		if u.Scheme == "" {
			return fmt.Errorf("If specified, HTTPAdvertise must include scheme (http:// or https://)")
		}
		if u.Hostname() == "" {
			return fmt.Errorf("If specified, HTTPAdvertise must include host name")
		}
		if u.Port() == "" {
			return fmt.Errorf("If specified, HTTPAdvertise must include port number")
		}
		if u.Path != "" {
			return fmt.Errorf("If specified, HTTPAdvertise must not specify a path")
		}
		if this.InstanceWriteBufferSize <= 0 {
			this.BufferInstanceWrites = false
		}
	}
	if this.ConsulMaxKVsPerTransaction < ConsulKVsPerCluster {
		this.ConsulMaxKVsPerTransaction = ConsulKVsPerCluster
	} else if this.ConsulMaxKVsPerTransaction > ConsulMaxTransactionOps {
		this.ConsulMaxKVsPerTransaction = ConsulMaxTransactionOps
	}
	if err := this.normalizeAndValidateConsul(); err != nil {
		return err
	}
	if this.DeadInstancePollSecondsMultiplyFactor < 1 {
		return fmt.Errorf("DeadInstancePollSecondsMultiplyFactor can not be smaller than 1")
	}

	if this.DeadInstancePollSecondsMax < this.InstancePollSeconds {
		return fmt.Errorf(("DeadInstancePollSecondsMax can not be smaller than InstancePollSeconds"))
	}
	return nil
}

func (this *Configuration) IsSQLite() bool {
	return strings.Contains(this.BackendDB, "sqlite")
}

func (this *Configuration) IsMySQL() bool {
	return this.BackendDB == "mysql" || this.BackendDB == ""
}

// ValidateRaft validates the mandatory server runtime; offline admin commands do not start Raft.
func (this *Configuration) ValidateRaft() error {
	if this.RaftDataDir == "" {
		return fmt.Errorf("RaftDataDir must be defined for server startup")
	}
	this.RaftNodeID = strings.TrimSpace(this.RaftNodeID)
	if this.RaftNodeID == "" {
		return fmt.Errorf("RaftNodeID must be defined for server startup")
	}
	if strings.ContainsAny(this.RaftNodeID, " \t\r\n") {
		return fmt.Errorf("RaftNodeID must not contain whitespace")
	}
	if this.RaftBind == "" {
		return fmt.Errorf("RaftBind must be defined for server startup")
	}
	normalizedBind, err := NormalizeRaftAddress(this.RaftBind, this.DefaultRaftPort)
	if err != nil {
		return fmt.Errorf("RaftBind is invalid: %w", err)
	}
	this.RaftBind = normalizedBind
	if this.RaftAdvertise == "" {
		this.RaftAdvertise = this.RaftBind
	} else {
		normalizedAdvertise, err := NormalizeRaftAddress(this.RaftAdvertise, this.DefaultRaftPort)
		if err != nil {
			return fmt.Errorf("RaftAdvertise is invalid: %w", err)
		}
		this.RaftAdvertise = normalizedAdvertise
	}
	return nil
}

func decodeConfiguration(reader io.Reader, configuration *Configuration) error {
	yamlDecoder := yaml.NewDecoder(reader, yaml.UseOrderedMap())
	var document any
	if err := yamlDecoder.Decode(&document); err != nil {
		return err
	}
	var extraDocument any
	if err := yamlDecoder.Decode(&extraDocument); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple configuration documents are not supported")
		}
		return err
	}

	jsonDocument, err := yaml.MarshalWithOptions(document, yaml.JSON())
	if err != nil {
		return err
	}
	jsonDecoder := json.NewDecoder(bytes.NewReader(jsonDocument))
	jsonDecoder.DisallowUnknownFields()
	if err := jsonDecoder.Decode(configuration); err != nil {
		return err
	}
	if err := jsonDecoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing configuration content is not supported")
		}
		return err
	}
	return nil
}

// readInto applies one configuration file to the provided candidate.
func readInto(fileName string, candidate *Configuration) error {
	if fileName == "" {
		return fmt.Errorf("empty file name")
	}
	file, err := os.Open(fileName)
	if err != nil {
		return fmt.Errorf("open configuration %s: %w", fileName, err)
	}
	defer file.Close()

	if err := decodeConfiguration(file, candidate); err != nil {
		return fmt.Errorf("decode configuration %s: %w", fileName, err)
	}
	if err := candidate.postReadAdjustments(); err != nil {
		return fmt.Errorf("adjust configuration %s: %w", fileName, err)
	}
	return nil
}

func cloneConfiguration(configuration *Configuration) (*Configuration, error) {
	data, err := json.Marshal(configuration)
	if err != nil {
		return nil, fmt.Errorf("marshal configuration clone: %w", err)
	}
	cloned := new(Configuration)
	if err := json.Unmarshal(data, cloned); err != nil {
		return nil, fmt.Errorf("unmarshal configuration clone: %w", err)
	}
	return cloned, nil
}

func applyFiles(fileNames []string, skipMissing bool) (*Configuration, error) {
	candidate, err := cloneConfiguration(Config)
	if err != nil {
		return Config, err
	}
	appliedFiles := make([]string, 0, len(fileNames))
	for _, fileName := range fileNames {
		if fileName == "" {
			continue
		}
		if err := readInto(fileName, candidate); err != nil {
			if skipMissing && errors.Is(err, os.ErrNotExist) {
				continue
			}
			return Config, err
		}
		appliedFiles = append(appliedFiles, fileName)
	}
	if raftConfigurationLocked {
		if err := candidate.ValidateRaft(); err != nil {
			return Config, err
		}
		if candidate.RaftNodeID != Config.RaftNodeID || candidate.RaftDataDir != Config.RaftDataDir ||
			candidate.RaftBind != Config.RaftBind || candidate.RaftAdvertise != Config.RaftAdvertise ||
			candidate.HTTPAdvertise != Config.HTTPAdvertise || candidate.ListenAddress != Config.ListenAddress ||
			candidate.DefaultRaftPort != Config.DefaultRaftPort {
			return Config, fmt.Errorf("raft identity and address changes require a process restart")
		}
	}
	if telemetryConfigurationLocked && (candidate.OTelTraceEndpoint != Config.OTelTraceEndpoint || candidate.OTelTraceSampleRatio != Config.OTelTraceSampleRatio) {
		return Config, fmt.Errorf("telemetry configuration changes require a process restart")
	}
	*Config = *candidate
	for _, fileName := range appliedFiles {
		log.Infof("Read config: %s", fileName)
	}
	return Config, nil
}

// Read reads configuration from zero, either, some or all given files, in order of input.
// A file can override configuration provided in previous file.
func Read(fileNames ...string) (*Configuration, error) {
	configuration, err := applyFiles(fileNames, true)
	if err != nil {
		return configuration, err
	}
	readFileNames = fileNames
	return configuration, nil
}

// ForceRead reads configuration from a required file.
func ForceRead(fileName string) (*Configuration, error) {
	configuration, err := applyFiles([]string{fileName}, false)
	if err != nil {
		return configuration, err
	}
	readFileNames = []string{fileName}
	return configuration, nil
}

// Reload re-reads configuration from last used files
func Reload(extraFileNames ...string) (*Configuration, error) {
	fileNames := make([]string, 0, len(readFileNames)+len(extraFileNames))
	fileNames = append(fileNames, readFileNames...)
	fileNames = append(fileNames, extraFileNames...)
	return applyFiles(fileNames, true)
}

// MarkConfigurationLoaded is called once configuration has first been loaded.
// Listeners on ConfigurationLoaded will get a notification
func MarkConfigurationLoaded() {
	telemetryConfigurationLocked = true
	go func() {
		for {
			configurationLoaded <- true
		}
	}()
	// wait for it
	<-configurationLoaded
}

// WaitForConfigurationToBeLoaded does just that. It will return after
// the configuration file has been read off disk.
func WaitForConfigurationToBeLoaded() {
	<-configurationLoaded
}
