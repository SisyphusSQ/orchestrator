package config

// Configuration is the complete layered orchestrator configuration.
type Configuration struct {
	Observability  ObservabilityConfiguration  `json:"observability"`
	Logging        LoggingConfiguration        `json:"logging"`
	Server         ServerConfiguration         `json:"server"`
	Raft           RaftConfiguration           `json:"raft"`
	MySQL          MySQLConfiguration          `json:"mysql"`
	Metadata       MetadataConfiguration       `json:"metadata"`
	Topology       TopologyConfiguration       `json:"topology"`
	Authentication AuthenticationConfiguration `json:"authentication"`
	Agents         AgentsConfiguration         `json:"agents"`
	PseudoGTID     PseudoGTIDConfiguration     `json:"pseudoGTID"`
	Hooks          HooksConfiguration          `json:"hooks"`
	OSC            OSCConfiguration            `json:"osc"`
	Audit          AuditConfiguration          `json:"audit"`
	Consul         ConsulConfiguration         `json:"consul"`
}

type ObservabilityConfiguration struct {
	Tracing TracingConfiguration `json:"tracing"`
}

type TracingConfiguration struct {
	Endpoint    string  `json:"endpoint"`
	SampleRatio float64 `json:"sampleRatio"`
}

type LoggingConfiguration struct {
	Debug  bool                `json:"debug"`
	Syslog SyslogConfiguration `json:"syslog"`
}

type SyslogConfiguration struct {
	Enabled bool `json:"enabled"`
}

type ServerConfiguration struct {
	Listen           ListenConfiguration           `json:"listen"`
	HTTPAdvertise    string                        `json:"httpAdvertise"`
	URLPrefix        string                        `json:"urlPrefix"`
	ReadOnly         bool                          `json:"readOnly"`
	TLS              TLSConfiguration              `json:"tls"`
	Status           StatusConfiguration           `json:"status"`
	Web              WebConfiguration              `json:"web"`
	ResponseIdentity ResponseIdentityConfiguration `json:"responseIdentity"`
}

type ListenConfiguration struct {
	Address string `json:"address"`
	Socket  string `json:"socket"`
}

type TLSConfiguration struct {
	Enabled        bool     `json:"enabled"`
	MutualTLS      bool     `json:"mutualTLS"`
	SkipVerify     bool     `json:"skipVerify"`
	PrivateKeyFile string   `json:"privateKeyFile"`
	CertFile       string   `json:"certFile"`
	CAFile         string   `json:"caFile"`
	ValidOUs       []string `json:"validOUs"`
}

type StatusConfiguration struct {
	Endpoint string `json:"endpoint"`
	VerifyOU bool   `json:"verifyOU"`
}

type WebConfiguration struct {
	Message                string `json:"message"`
	RemoveTextFromHostname string `json:"removeTextFromHostname"`
}

type ResponseIdentityConfiguration struct {
	Mode   string `json:"mode"`
	Custom string `json:"custom"`
}

type RaftConfiguration struct {
	NodeID      string `json:"nodeID"`
	Bind        string `json:"bind"`
	Advertise   string `json:"advertise"`
	DataDir     string `json:"dataDir"`
	DefaultPort int    `json:"defaultPort"`
}

type MySQLConfiguration struct {
	ConnectTimeoutSeconds     int `json:"connectTimeoutSeconds"`
	ConnectionLifetimeSeconds int `json:"connectionLifetimeSeconds"`
}

type MetadataConfiguration struct {
	Type   string                      `json:"type"`
	SQLite SQLiteMetadataConfiguration `json:"sqlite"`
	Schema MetadataSchemaConfiguration `json:"schema"`
	MySQL  MetadataMySQLConfiguration  `json:"mysql"`
}

type SQLiteMetadataConfiguration struct {
	DataFile string `json:"dataFile"`
}

type MetadataSchemaConfiguration struct {
	SkipUpdate                 bool `json:"skipUpdate"`
	PanicOnDifferentDeployment bool `json:"panicOnDifferentDeployment"`
}

type MetadataMySQLConfiguration struct {
	Host                  string `json:"host"`
	MaxPoolConnections    int    `json:"maxPoolConnections"`
	Port                  uint   `json:"port"`
	Database              string `json:"database"`
	User                  string `json:"user"`
	Password              string `json:"password"`
	CredentialsConfigFile string `json:"credentialsConfigFile"`
	SSLPrivateKeyFile     string `json:"sslPrivateKeyFile"`
	SSLCertFile           string `json:"sslCertFile"`
	SSLCAFile             string `json:"sslCAFile"`
	SSLSkipVerify         bool   `json:"sslSkipVerify"`
	UseMutualTLS          bool   `json:"useMutualTLS"`
	ReadTimeoutSeconds    int    `json:"readTimeoutSeconds"`
	RejectReadOnly        bool   `json:"rejectReadOnly"`
	MaxAllowedPacket      int32  `json:"maxAllowedPacket"`
}

type TopologyConfiguration struct {
	MySQL          TopologyMySQLConfiguration      `json:"mysql"`
	Replication    ReplicationConfiguration        `json:"replication"`
	Discovery      DiscoveryConfiguration          `json:"discovery"`
	WriteBuffer    WriteBufferConfiguration        `json:"writeBuffer"`
	Compatibility  CompatibilityConfiguration      `json:"compatibility"`
	Snapshot       SnapshotConfiguration           `json:"snapshot"`
	Hostname       HostnameConfiguration           `json:"hostname"`
	Candidate      CandidateConfiguration          `json:"candidate"`
	Classification ClassificationConfiguration     `json:"classification"`
	Pools          PoolConfiguration               `json:"pools"`
	Analysis       AnalysisConfiguration           `json:"analysis"`
	Operations     TopologyOperationsConfiguration `json:"operations"`
}

type TopologyMySQLConfiguration struct {
	User                        string `json:"user"`
	Password                    string `json:"password"`
	CredentialsConfigFile       string `json:"credentialsConfigFile"`
	SSLPrivateKeyFile           string `json:"sslPrivateKeyFile"`
	SSLCertFile                 string `json:"sslCertFile"`
	SSLCAFile                   string `json:"sslCAFile"`
	SSLSkipVerify               bool   `json:"sslSkipVerify"`
	UseMutualTLS                bool   `json:"useMutualTLS"`
	UseMixedTLS                 bool   `json:"useMixedTLS"`
	MaxAllowedPacket            int32  `json:"maxAllowedPacket"`
	TLSCacheTTLFactor           uint   `json:"tlsCacheTTLFactor"`
	DefaultPort                 int    `json:"defaultPort"`
	DiscoveryReadTimeoutSeconds int    `json:"discoveryReadTimeoutSeconds"`
	ReadTimeoutSeconds          int    `json:"readTimeoutSeconds"`
}

type ReplicationConfiguration struct {
	LagQuery         string `json:"lagQuery"`
	CredentialsQuery string `json:"credentialsQuery"`
}

type DiscoveryConfiguration struct {
	UseShowReplicaHosts        bool     `json:"useShowReplicaHosts"`
	PollSeconds                uint     `json:"pollSeconds"`
	DeadPollSecondsFactor      float32  `json:"deadPollSecondsFactor"`
	DeadPollMaxSeconds         uint     `json:"deadPollMaxSeconds"`
	DeadMaxConcurrency         uint     `json:"deadMaxConcurrency"`
	DeadLogsEnabled            bool     `json:"deadLogsEnabled"`
	ReasonableCheckSeconds     uint     `json:"reasonableCheckSeconds"`
	UnseenForgetHours          uint     `json:"unseenForgetHours"`
	MaxConcurrency             uint     `json:"maxConcurrency"`
	QueueCapacity              uint     `json:"queueCapacity"`
	Seeds                      []string `json:"seeds"`
	IgnoreReplicaHostnames     []string `json:"ignoreReplicaHostnames"`
	IgnoreMasterHostnames      []string `json:"ignoreMasterHostnames"`
	IgnoreHostnames            []string `json:"ignoreHostnames"`
	IgnoreReplicationUsernames []string `json:"ignoreReplicationUsernames"`
	FilterLogsEnabled          bool     `json:"filterLogsEnabled"`
}

type WriteBufferConfiguration struct {
	Size                      int  `json:"size"`
	Enabled                   bool `json:"enabled"`
	FlushIntervalMilliseconds int  `json:"flushIntervalMilliseconds"`
}

type CompatibilityConfiguration struct {
	SkipMaxScaleCheck          bool `json:"skipMaxScaleCheck"`
	LowerReplicaVersionAllowed bool `json:"lowerReplicaVersionAllowed"`
}

type SnapshotConfiguration struct {
	IntervalHours uint `json:"intervalHours"`
}

type HostnameConfiguration struct {
	ResolveMethod                  string `json:"resolveMethod"`
	MySQLResolveMethod             string `json:"mysqlResolveMethod"`
	SkipBinlogServerUnresolveCheck bool   `json:"skipBinlogServerUnresolveCheck"`
	ResolveExpiryMinutes           int    `json:"resolveExpiryMinutes"`
	RejectResolvePattern           string `json:"rejectResolvePattern"`
}

type CandidateConfiguration struct {
	ExpireMinutes uint `json:"expireMinutes"`
}

type ClassificationConfiguration struct {
	ClusterNameToAlias             map[string]string `json:"clusterNameToAlias"`
	DetectClusterAliasQuery        string            `json:"detectClusterAliasQuery"`
	DetectClusterDomainQuery       string            `json:"detectClusterDomainQuery"`
	DetectInstanceAliasQuery       string            `json:"detectInstanceAliasQuery"`
	DetectPromotionRuleQuery       string            `json:"detectPromotionRuleQuery"`
	DataCenterPattern              string            `json:"dataCenterPattern"`
	RegionPattern                  string            `json:"regionPattern"`
	PhysicalEnvironmentPattern     string            `json:"physicalEnvironmentPattern"`
	DetectDataCenterQuery          string            `json:"detectDataCenterQuery"`
	DetectRegionQuery              string            `json:"detectRegionQuery"`
	DetectPhysicalEnvironmentQuery string            `json:"detectPhysicalEnvironmentQuery"`
	DetectSemiSyncEnforcedQuery    string            `json:"detectSemiSyncEnforcedQuery"`
}

type PoolConfiguration struct {
	SupportFuzzyHostnames bool `json:"supportFuzzyHostnames"`
	ExpiryMinutes         uint `json:"expiryMinutes"`
}

type AnalysisConfiguration struct {
	ReduceCount bool `json:"reduceCount"`
}

type TopologyOperationsConfiguration struct {
	UseSuperReadOnly               bool `json:"useSuperReadOnly"`
	BulkWaitTimeoutSeconds         uint `json:"bulkWaitTimeoutSeconds"`
	MaxConcurrentReplicaOperations int  `json:"maxConcurrentReplicaOperations"`
}

type AuthenticationConfiguration struct {
	Method              string                           `json:"method"`
	Basic               BasicAuthenticationConfiguration `json:"basic"`
	Proxy               ProxyAuthenticationConfiguration `json:"proxy"`
	Power               AuthorizedSubjectsConfiguration  `json:"power"`
	ConfigurationAdmins AuthorizedSubjectsConfiguration  `json:"configurationAdmins"`
	AccessToken         AccessTokenConfiguration         `json:"accessToken"`
}

type BasicAuthenticationConfiguration struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type ProxyAuthenticationConfiguration struct {
	UserHeader string `json:"userHeader"`
}

type AuthorizedSubjectsConfiguration struct {
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}

type AccessTokenConfiguration struct {
	UseExpirySeconds uint `json:"useExpirySeconds"`
	ExpiryMinutes    uint `json:"expiryMinutes"`
}

type AgentsConfiguration struct {
	ServeHTTP                 bool                  `json:"serveHTTP"`
	ServerPort                string                `json:"serverPort"`
	PollMinutes               uint                  `json:"pollMinutes"`
	UnseenForgetHours         uint                  `json:"unseenForgetHours"`
	StaleSeedFailMinutes      uint                  `json:"staleSeedFailMinutes"`
	SeedWaitSecondsBeforeSend int64                 `json:"seedWaitSecondsBeforeSend"`
	TLS                       AgentTLSConfiguration `json:"tls"`
}

type AgentTLSConfiguration struct {
	Enabled        bool     `json:"enabled"`
	MutualTLS      bool     `json:"mutualTLS"`
	SkipVerify     bool     `json:"skipVerify"`
	PrivateKeyFile string   `json:"privateKeyFile"`
	CertFile       string   `json:"certFile"`
	CAFile         string   `json:"caFile"`
	ValidOUs       []string `json:"validOUs"`
}

type PseudoGTIDConfiguration struct {
	Auto                    bool     `json:"auto"`
	Pattern                 string   `json:"pattern"`
	PatternIsFixedSubstring bool     `json:"patternIsFixedSubstring"`
	MonotonicHint           string   `json:"monotonicHint"`
	DetectQuery             string   `json:"detectQuery"`
	BinlogEventsChunkSize   int      `json:"binlogEventsChunkSize"`
	SkipBinlogContaining    []string `json:"skipBinlogContaining"`
}

type HooksConfiguration struct {
	ShellCommand string `json:"shellCommand"`
}

type OSCConfiguration struct {
	IgnoreHostnames []string `json:"ignoreHostnames"`
}

type AuditConfiguration struct {
	LogFile   string `json:"logFile"`
	ToSyslog  bool   `json:"toSyslog"`
	ToBackend bool   `json:"toBackend"`
	PurgeDays uint   `json:"purgeDays"`
}

type ConsulConfiguration struct {
	Address            string                 `json:"address"`
	Scheme             string                 `json:"scheme"`
	ACLToken           string                 `json:"aclToken"`
	Datacenter         string                 `json:"datacenter"`
	HTTPTimeoutSeconds int                    `json:"httpTimeoutSeconds"`
	TLS                ConsulTLSConfiguration `json:"tls"`
	KV                 ConsulKVConfiguration  `json:"kv"`
}

type ConsulTLSConfiguration struct {
	CAFile         string `json:"caFile"`
	CAPath         string `json:"caPath"`
	CertFile       string `json:"certFile"`
	PrivateKeyFile string `json:"privateKeyFile"`
	ServerName     string `json:"serverName"`
	SkipVerify     bool   `json:"skipVerify"`
}

type ConsulKVConfiguration struct {
	Provider                    string `json:"provider"`
	MaxKVsPerTransaction        int    `json:"maxKVsPerTransaction"`
	ClusterMasterPrefix         string `json:"clusterMasterPrefix"`
	CrossDataCenterDistribution bool   `json:"crossDataCenterDistribution"`
}
