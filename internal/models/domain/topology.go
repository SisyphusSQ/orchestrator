package domain

type OperationGTIDHint string

const (
	GTIDHintDeny    = "NoGTID"
	GTIDHintNeutral = "GTIDHintNeutral"
	GTIDHintForce   = "GTIDHintForce"
)

type ReplicationCredentials struct {
	User      string
	Password  string
	SSLCert   string
	SSLKey    string
	SSLCaCert string
}

// GroupReplicationMember describes one member returned by MySQL Group Replication discovery.
type GroupReplicationMember struct {
	UUID               string
	Host               string
	Port               uint16
	State              string
	Role               string
	GroupName          string
	SinglePrimaryGroup bool
}
