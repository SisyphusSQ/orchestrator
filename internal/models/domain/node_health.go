package domain

// NodeHealth describes the persisted liveness state shared by process logic
// and the metadata repository.
type NodeHealth struct {
	Hostname        string
	Token           string
	AppVersion      string
	FirstSeenActive string
	LastSeenActive  string
	DBBackend       string
}
