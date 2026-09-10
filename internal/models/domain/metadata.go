package domain

// InstanceIdentity identifies one managed MySQL instance across repository
// and business boundaries.
type InstanceIdentity struct {
	Hostname string
	Port     int
}

// ClusterAlias identifies the explicit alias owned by a cluster.
type ClusterAlias struct {
	ClusterName string
	Alias       string
}

// CandidateInstance describes a failover candidate registration.
type CandidateInstance struct {
	Hostname            string
	Port                int
	PromotionRule       string
	LastSuggested       string
	PromotionRuleExpiry string
}

// EquivalentCoordinates identifies a binlog position known to be equivalent
// to positions on other instances.
type EquivalentCoordinates struct {
	Hostname   string
	Port       int
	BinlogFile string
	BinlogPos  int64
}

// DowntimeRecord describes a persisted downtime interval.
type DowntimeRecord struct {
	Hostname       string
	Port           int
	BeginTimestamp string
	EndTimestamp   string
	Owner          string
	Reason         string
}

// ClusterPoolInstance describes one pool membership.
type ClusterPoolInstance struct {
	ClusterName  string
	ClusterAlias string
	Pool         string
	Hostname     string
	Port         int
}

// PoolInstancesSubmission describes the latest submitted hosts for a pool.
type PoolInstancesSubmission struct {
	Pool         string
	RegisteredAt string
	Hosts        string
}

type AuditRecord struct {
	ID        int64
	Timestamp string
	Type      string
	Hostname  string
	Port      int
	Message   string
}

type SuggestedClusterAlias struct {
	SuggestedAlias string
	Hostname       string
	Port           int
}

type HostnameResolve struct {
	Hostname         string
	ResolvedHostname string
}

type HostnameUnresolve struct {
	Hostname           string
	UnresolvedHostname string
}

type MissingHostnameResolve struct {
	UnresolvedHostname string
	Port               int
}

type HostnameIP struct {
	IPv4 string
	IPv6 string
}

type InstanceTag struct {
	Name  string
	Value string
}

type InstanceClusterAttributes struct {
	ClusterName           string
	SuggestedClusterAlias string
	ReplicationDepth      uint
	MasterHost            string
	MasterPort            int
	AncestryUUID          string
	ExecutedGTIDSet       string
}

type MasterHostnameResolve struct {
	Hostname         string
	ResolvedHostname string
}

type SnapshotCount struct {
	Hostname string
	Count    int
}

type ClusterInfoRecord struct {
	ClusterName    string
	CountInstances uint
	Alias          string
	DomainName     string
}

type MinimalInstanceRecord struct {
	Hostname    string
	Port        int
	MasterHost  string
	MasterPort  int
	ClusterName string
}

type InstanceCoordinatesRecord struct {
	BinaryLogFile string
	BinaryLogPos  int64
	RelayLogFile  string
	RelayLogPos   int64
}

type RelayCoordinatesRecord struct {
	RelayLogFile string
	RelayLogPos  int64
}

type MaintenanceRecord struct {
	ID             uint
	Hostname       string
	Port           int
	BeginTimestamp string
	SecondsElapsed uint
	Active         bool
	Owner          string
	Reason         string
}
