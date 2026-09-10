package domain

import "time"

// DatabasePoolStats is the repository-independent snapshot consumed by metrics.
type DatabasePoolStats struct {
	InUse              int
	Idle               int
	MaxOpenConnections int
	WaitCount          int64
	WaitDuration       time.Duration
}
