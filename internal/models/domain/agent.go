package domain

// LogicalVolume describes an LVM volume exposed by an agent.
type LogicalVolume struct {
	Name            string
	GroupName       string
	Path            string
	IsSnapshot      bool
	SnapshotPercent float64
}

// Mount describes a file system mount point exposed by an agent.
type Mount struct {
	Path           string
	Device         string
	LVPath         string
	FileSystem     string
	IsMounted      bool
	DiskUsage      int64
	MySQLDataPath  string
	MySQLDiskUsage int64
}

// Agent presents the business-facing data returned by an agent.
type Agent struct {
	Hostname                string
	Port                    int
	Token                   string
	LastSubmitted           string
	AvailableLocalSnapshots []string
	AvailableSnapshots      []string
	LogicalVolumes          []LogicalVolume
	MountPoint              Mount
	MySQLRunning            bool
	MySQLDiskUsage          int64
	MySQLPort               int64
	MySQLDatadirDiskFree    int64
	MySQLErrorLogTail       []string
}

// SeedOperation describes a seed workflow.
type SeedOperation struct {
	SeedId         int64
	TargetHostname string
	SourceHostname string
	StartTimestamp string
	EndTimestamp   string
	IsComplete     bool
	IsSuccessful   bool
}

// SeedOperationState describes one step of a seed workflow.
type SeedOperationState struct {
	SeedStateId    int64
	SeedId         int64
	StateTimestamp string
	Action         string
	ErrorMessage   string
}
