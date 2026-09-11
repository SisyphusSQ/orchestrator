// Package mysql owns MySQL-version-specific query and status-field vocabulary.
package mysql

import (
	"maps"
	"strconv"
	"strings"
)

// Key identifies one version-dependent MySQL query or result field.
type Key int

type queryStringProvider struct {
	queries map[Key]string
}

const (
	LogSlaveUpdates Key = iota
	StartSlave
	ShowSlaveStatus
	SlaveIORunning
	SlaveSQLRunning
	ShowMasterStatus
	MasterGTIDInfo
	MasterUser
	SlaveIOState
	MasterLogFile
	ReadMasterLogPos
	RelayMasterLogFile
	RelayMasterLogPosition
	MasterUUID
	MasterHost
	MasterPort
	SecondsBehindMaster
	MasterSSLAllowed
	ShowSlaveHosts

	StopSlaveIOThread
	StopSlaveSQLThread
	StartSlaveSQLThread
	StartSlaveIOThread
	StopSlave
	StartSlaveUntilMasterLog
	MasterUserParam
	MasterPasswordParam
	MasterSSLCAParam
	MasterSSLCertParam
	MasterSSL
	MasterSSLKeyParam
	ResetSlave
	ChangeMasterToMasterHost
	ResetSlave50603All
	ResetMaster
	SelectMasterPosWait
	ChangeMasterToWithParams
	ChangeMasterToMasterDelay
	SetSQLSlaveSkipCounter
	ChangeMasterToGetSourcePublicKey
	SelectUserHost
	MasterHostAssign
	MasterPortAssign
	MasterLogFileAssign
	MasterLogPosAssign
	MasterAutoPositionAssign
	MasterSSLCapathParam
	MasterSSLCipherParam
	MasterSSLCRLParam
	MasterSSLCRLPathParam
	MasterSSLVerifyServerCertParam
	MasterTLSVersionParam
	MasterTLSCipherSuitesParam
	MasterPublicKeyPathParam
	MasterGetSourcePublicKeyParam
	ReplicaStatusSSLCAFile
	ReplicaStatusSSLCAPath
	ReplicaStatusSSLCert
	ReplicaStatusSSLCipher
	ReplicaStatusSSLCRLFile
	ReplicaStatusSSLCRLPath
	ReplicaStatusSSLKey
	ReplicaStatusSSLVerifyServerCert
	ReplicaStatusTLSVersion
	ReplicaStatusTLSCipherSuites
	ReplicaStatusPublicKeyPath
	ReplicaStatusGetSourcePublicKey
	keyCount
)

// Query returns the query or result-field spelling for a MySQL version.
func Query(version string, key Key) string {
	return providerForVersion(version).queries[key]
}

// queryStringsPre8014 covers MySQL 5.6, 5.7 and 8.0 before 8.0.14.
// These releases use the legacy MASTER/SLAVE replication vocabulary.
var queryStringsPre8014 = map[Key]string{
	LogSlaveUpdates:        "log_slave_updates",
	StartSlave:             "start slave",
	ShowSlaveStatus:        "show slave status",
	SlaveIORunning:         "Slave_IO_Running",
	SlaveSQLRunning:        "Slave_SQL_Running",
	ShowMasterStatus:       "show master status",
	MasterGTIDInfo:         "select @@global.gtid_mode, @@global.server_uuid, @@global.gtid_executed, @@global.gtid_purged, @@global.master_info_repository = 'TABLE', @@global.binlog_row_image",
	MasterUser:             "Master_User",
	SlaveIOState:           "Slave_IO_State",
	MasterLogFile:          "Master_Log_File",
	ReadMasterLogPos:       "Read_Master_Log_Pos",
	RelayMasterLogFile:     "Relay_Master_Log_File",
	RelayMasterLogPosition: "Exec_Master_Log_Pos",
	MasterUUID:             "Master_UUID",
	MasterHost:             "Master_Host",
	MasterPort:             "Master_Port",
	SecondsBehindMaster:    "Seconds_Behind_Master",
	MasterSSLAllowed:       "Master_SSL_Allowed",
	ShowSlaveHosts:         "show slave hosts",

	StopSlaveIOThread:                "stop slave io_thread",
	StopSlaveSQLThread:               "stop slave sql_thread",
	StartSlaveSQLThread:              "start slave sql_thread",
	StartSlaveIOThread:               "start slave io_thread",
	StopSlave:                        "stop slave",
	StartSlaveUntilMasterLog:         "start slave until master_log_file=?, master_log_pos=?",
	MasterUserParam:                  "master_user = ?",
	MasterPasswordParam:              "master_password = ?",
	MasterSSLCAParam:                 "master_ssl_ca = ?",
	MasterSSLCertParam:               "master_ssl_cert = ?",
	MasterSSL:                        "master_ssl",
	MasterSSLKeyParam:                "master_ssl_key = ?",
	ResetSlave:                       "reset slave",
	ChangeMasterToMasterHost:         "change master to master_host='_'",
	ResetSlave50603All:               "reset slave /*!50603 all */",
	ResetMaster:                      "reset master",
	SelectMasterPosWait:              "select master_pos_wait(?, ?)",
	ChangeMasterToWithParams:         "change master to %s",
	ChangeMasterToMasterDelay:        "change master to master_delay=%d",
	SetSQLSlaveSkipCounter:           "set global sql_slave_skip_counter := 1",
	ChangeMasterToGetSourcePublicKey: "select 1",
	SelectUserHost:                   "select user, substring_index(host, ':', 1) as slave_hostname from information_schema.processlist where command IN ('Binlog Dump', 'Binlog Dump GTID')",

	MasterHostAssign:                 "master_host=?",
	MasterPortAssign:                 "master_port=?",
	MasterLogFileAssign:              "master_log_file=?",
	MasterLogPosAssign:               "master_log_pos=?",
	MasterAutoPositionAssign:         "master_auto_position=?",
	MasterSSLCapathParam:             "master_ssl_capath = ?",
	MasterSSLCipherParam:             "master_ssl_cipher = ?",
	MasterSSLCRLParam:                "master_ssl_crl = ?",
	MasterSSLCRLPathParam:            "master_ssl_crlpath = ?",
	MasterSSLVerifyServerCertParam:   "master_ssl_verify_server_cert = ?",
	MasterTLSVersionParam:            "master_tls_version = ?",
	MasterTLSCipherSuitesParam:       "master_tls_ciphersuites = ?",
	MasterPublicKeyPathParam:         "master_public_key_path = ?",
	MasterGetSourcePublicKeyParam:    "get_master_public_key = ?",
	ReplicaStatusSSLCAFile:           "Master_SSL_CA_File",
	ReplicaStatusSSLCAPath:           "Master_SSL_CA_Path",
	ReplicaStatusSSLCert:             "Master_SSL_Cert",
	ReplicaStatusSSLCipher:           "Master_SSL_Cipher",
	ReplicaStatusSSLCRLFile:          "Master_SSL_CRL_File",
	ReplicaStatusSSLCRLPath:          "Master_SSL_CRL_Path",
	ReplicaStatusSSLKey:              "Master_SSL_Key",
	ReplicaStatusSSLVerifyServerCert: "Master_SSL_Verify_Server_Cert",
	ReplicaStatusTLSVersion:          "Master_TLS_Version",
	ReplicaStatusTLSCipherSuites:     "Master_TLS_Ciphersuites",
	ReplicaStatusPublicKeyPath:       "Master_public_key_path",
	ReplicaStatusGetSourcePublicKey:  "Get_master_public_key",
}

var queryStrings8014 = func() map[Key]string {
	m := make(map[Key]string, len(queryStringsPre8014))

	// copy everything from version < 8.0.14
	maps.Copy(m, queryStringsPre8014)

	// change for 8.0.14 and newer 8.0
	m[SelectUserHost] = "select user, substring_index(host, ':', 1) as slave_hostname from performance_schema.processlist where command IN ('Binlog Dump', 'Binlog Dump GTID')"

	return m

}()

var queryStrings84 = map[Key]string{
	LogSlaveUpdates:        "log_replica_updates",
	StartSlave:             "start replica",
	ShowSlaveStatus:        "show replica status",
	SlaveIORunning:         "Replica_IO_Running",
	SlaveSQLRunning:        "Replica_SQL_Running",
	ShowMasterStatus:       "show binary log status",
	MasterGTIDInfo:         "select @@global.gtid_mode, @@global.server_uuid, @@global.gtid_executed, @@global.gtid_purged, 1, @@global.binlog_row_image",
	MasterUser:             "Source_User",
	SlaveIOState:           "Replica_IO_State",
	MasterLogFile:          "Source_Log_File",
	ReadMasterLogPos:       "Read_Source_Log_Pos",
	RelayMasterLogFile:     "Relay_Source_Log_File",
	RelayMasterLogPosition: "Exec_Source_Log_Pos",
	MasterUUID:             "Source_UUID",
	MasterHost:             "Source_Host",
	MasterPort:             "Source_Port",
	SecondsBehindMaster:    "Seconds_Behind_Source",
	MasterSSLAllowed:       "Source_SSL_Allowed",
	ShowSlaveHosts:         "show replicas",

	StopSlaveIOThread:                "stop replica io_thread",
	StopSlaveSQLThread:               "stop replica sql_thread",
	StartSlaveSQLThread:              "start replica sql_thread",
	StartSlaveIOThread:               "start replica io_thread",
	StopSlave:                        "stop replica",
	StartSlaveUntilMasterLog:         "start replica until source_log_file=?, source_log_pos=?",
	MasterUserParam:                  "source_user = ?",
	MasterPasswordParam:              "source_password = ?",
	MasterSSLCAParam:                 "source_ssl_ca = ?",
	MasterSSLCertParam:               "source_ssl_cert = ?",
	MasterSSL:                        "source_ssl",
	MasterSSLKeyParam:                "source_ssl_key = ?",
	ResetSlave:                       "reset replica",
	ChangeMasterToMasterHost:         "change replication source to source_host='_'",
	ResetSlave50603All:               "reset replica /*!50603 all */",
	ResetMaster:                      "reset binary logs and gtids",
	SelectMasterPosWait:              "select source_pos_wait(?, ?)",
	ChangeMasterToWithParams:         "change replication source to %s",
	ChangeMasterToMasterDelay:        "change replication source to source_delay=%d",
	SetSQLSlaveSkipCounter:           "set global sql_replica_skip_counter := 1",
	ChangeMasterToGetSourcePublicKey: "change replication source to get_source_public_key=1",
	SelectUserHost:                   "select user, substring_index(host, ':', 1) as slave_hostname from performance_schema.processlist where command IN ('Binlog Dump', 'Binlog Dump GTID')",

	MasterHostAssign:                 "source_host=?",
	MasterPortAssign:                 "source_port=?",
	MasterLogFileAssign:              "source_log_file=?",
	MasterLogPosAssign:               "source_log_pos=?",
	MasterAutoPositionAssign:         "source_auto_position=?",
	MasterSSLCapathParam:             "source_ssl_capath = ?",
	MasterSSLCipherParam:             "source_ssl_cipher = ?",
	MasterSSLCRLParam:                "source_ssl_crl = ?",
	MasterSSLCRLPathParam:            "source_ssl_crlpath = ?",
	MasterSSLVerifyServerCertParam:   "source_ssl_verify_server_cert = ?",
	MasterTLSVersionParam:            "source_tls_version = ?",
	MasterTLSCipherSuitesParam:       "source_tls_ciphersuites = ?",
	MasterPublicKeyPathParam:         "source_public_key_path = ?",
	MasterGetSourcePublicKeyParam:    "get_source_public_key = ?",
	ReplicaStatusSSLCAFile:           "Source_SSL_CA_File",
	ReplicaStatusSSLCAPath:           "Source_SSL_CA_Path",
	ReplicaStatusSSLCert:             "Source_SSL_Cert",
	ReplicaStatusSSLCipher:           "Source_SSL_Cipher",
	ReplicaStatusSSLCRLFile:          "Source_SSL_CRL_File",
	ReplicaStatusSSLCRLPath:          "Source_SSL_CRL_Path",
	ReplicaStatusSSLKey:              "Source_SSL_Key",
	ReplicaStatusSSLVerifyServerCert: "Source_SSL_Verify_Server_Cert",
	ReplicaStatusTLSVersion:          "Source_TLS_Version",
	ReplicaStatusTLSCipherSuites:     "Source_TLS_Ciphersuites",
	ReplicaStatusPublicKeyPath:       "Source_public_key_path",
	ReplicaStatusGetSourcePublicKey:  "Get_source_public_key",
}

var queryStringProviderPre8014 = queryStringProvider{
	queries: queryStringsPre8014,
}

var queryStringProvider8014 = queryStringProvider{
	queries: queryStrings8014,
}

var queryStringProvider84 = queryStringProvider{
	queries: queryStrings84,
}

func providerForVersion(version string) queryStringProvider {
	if strings.Contains(strings.ToLower(version), "mariadb") {
		return queryStringProviderPre8014
	}
	major, minor, patch, ok := numericVersion(version)
	if !ok {
		// Preserve the historical fallback for non-MySQL version strings.
		return queryStringProviderPre8014
	}

	if versionAtLeast(major, minor, patch, 8, 4, 0) {
		return queryStringProvider84
	}
	if versionAtLeast(major, minor, patch, 8, 0, 14) {
		return queryStringProvider8014
	}

	return queryStringProviderPre8014
}

func numericVersion(version string) (major, minor, patch int, ok bool) {
	values := []*int{&major, &minor, &patch}
	start := 0
	for index, target := range values {
		end := start
		for end < len(version) && version[end] >= '0' && version[end] <= '9' {
			end++
		}
		if end == start {
			return 0, 0, 0, false
		}
		value, err := strconv.Atoi(version[start:end])
		if err != nil {
			return 0, 0, 0, false
		}
		*target = value
		if index == len(values)-1 || end >= len(version) || version[end] != '.' {
			return major, minor, patch, true
		}
		start = end + 1
	}
	return major, minor, patch, true
}

func versionAtLeast(major, minor, patch, wantMajor, wantMinor, wantPatch int) bool {
	if major != wantMajor {
		return major > wantMajor
	}
	if minor != wantMinor {
		return minor > wantMinor
	}
	return patch >= wantPatch
}
