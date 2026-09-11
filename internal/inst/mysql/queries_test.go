package mysql

import "testing"

func TestQueryUsesVersionSpecificReplicationVocabulary(t *testing.T) {
	tests := []struct {
		name    string
		version string
		key     Key
		want    string
	}{
		{name: "unknown version falls back to 8.0", version: "MariaDB", key: ShowSlaveStatus, want: "show slave status"},
		{name: "5.6 replication command", version: "5.6.51", key: StartSlaveUntilMasterLog, want: "start slave until master_log_file=?, master_log_pos=?"},
		{name: "5.6 source field", version: "5.6.51", key: MasterHost, want: "Master_Host"},
		{name: "5.7 replication status", version: "5.7.44-log", key: ShowSlaveStatus, want: "show slave status"},
		{name: "5.7 log updates variable", version: "5.7.44-log", key: LogSlaveUpdates, want: "log_slave_updates"},
		{name: "MariaDB 10.11 keeps legacy command", version: "10.11.8-MariaDB", key: ShowSlaveStatus, want: "show slave status"},
		{name: "MariaDB 11.4 keeps master field", version: "11.4.5-MariaDB", key: MasterHost, want: "Master_Host"},
		{name: "single digit 8.0 patch stays before 8.0.14", version: "8.0.9", key: SelectUserHost, want: "select user, substring_index(host, ':', 1) as slave_hostname from information_schema.processlist where command IN ('Binlog Dump', 'Binlog Dump GTID')"},
		{name: "pre 8.0.14 process list", version: "8.0.13", key: SelectUserHost, want: "select user, substring_index(host, ':', 1) as slave_hostname from information_schema.processlist where command IN ('Binlog Dump', 'Binlog Dump GTID')"},
		{name: "8.0.14 process list", version: "8.0.14", key: SelectUserHost, want: "select user, substring_index(host, ':', 1) as slave_hostname from performance_schema.processlist where command IN ('Binlog Dump', 'Binlog Dump GTID')"},
		{name: "8.4 command", version: "8.4.0", key: ShowSlaveStatus, want: "show replica status"},
		{name: "8.4 source field", version: "8.4.0", key: MasterHost, want: "Source_Host"},
		{name: "future 8.x uses source vocabulary", version: "8.10.0", key: MasterHost, want: "Source_Host"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Query(test.version, test.key); got != test.want {
				t.Fatalf("Query(%q, %d) = %q, want %q", test.version, test.key, got, test.want)
			}
		})
	}
}

func TestEveryQueryKeyHasAllVersionMappings(t *testing.T) {
	for _, version := range []string{"5.6.51", "5.7.44-log", "8.0.13", "8.0.14", "8.4.0"} {
		for key := Key(0); key < keyCount; key++ {
			if got := Query(version, key); got == "" {
				t.Errorf("Query(%q, %d) is empty", version, key)
			}
		}
	}
}
