package inst

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/observability"
	"github.com/openark/orchestrator/internal/repository/topology"
	"github.com/openark/orchestrator/internal/util"
	"github.com/sjmudd/stopwatch"
)

// logReadTopologyInstanceError logs an error, if applicable, for a ReadTopologyInstance operation.
func logReadTopologyInstanceError(instanceKey *InstanceKey, hint string, err error) error {
	if err == nil {
		return nil
	}
	if !util.ClearToLog("ReadTopologyInstance", instanceKey.StringCode()) {
		return err
	}
	var msg string
	if hint == "" {
		msg = fmt.Sprintf("ReadTopologyInstance(%+v): %+v", *instanceKey, err)
	} else {
		msg = fmt.Sprintf("ReadTopologyInstance(%+v) %+v: %+v",
			*instanceKey,
			strings.Replace(hint, "%", "%%", -1), // escape %
			err)
	}
	return log.Errorf("%s", msg)
}

// readReplicationTLSStatusFromShowReplicaRow copies TLS-related columns from SHOW SLAVE/REPLICA STATUS
// so ChangeMasterTo can replay them on CHANGE REPLICATION SOURCE / CHANGE MASTER.
func readReplicationTLSStatusFromShowReplicaRow(instance *Instance, m modeldomain.DynamicRow) {
	q := instance.QSP
	instance.ReplicationSSLCAFile = m.GetStringD(q.replica_status_ssl_ca_file(), "")
	instance.ReplicationSSLCAPath = m.GetStringD(q.replica_status_ssl_ca_path(), "")
	instance.ReplicationSSLCert = m.GetStringD(q.replica_status_ssl_cert(), "")
	instance.ReplicationSSLCipher = m.GetStringD(q.replica_status_ssl_cipher(), "")
	instance.ReplicationSSLCRLFile = m.GetStringD(q.replica_status_ssl_crl_file(), "")
	instance.ReplicationSSLCRLPath = m.GetStringD(q.replica_status_ssl_crl_path(), "")
	instance.ReplicationSSLKey = m.GetStringD(q.replica_status_ssl_key(), "")
	instance.ReplicationTLSVersion = m.GetStringD(q.replica_status_tls_version(), "")
	instance.ReplicationTLSCiphersuites = m.GetStringD(q.replica_status_tls_ciphersuites(), "")
	instance.ReplicationSourcePublicKeyPath = m.GetStringD(q.replica_status_public_key_path(), "")

	if v := m.GetStringD(q.replica_status_ssl_verify_server_cert(), ""); v != "" {
		instance.ReplicationSSLVerifyServerCert = modeldomain.NullBool{
			Valid: true,
			Bool:  strings.EqualFold(v, "Yes") || v == "1",
		}
	} else {
		instance.ReplicationSSLVerifyServerCert = modeldomain.NullBool{Valid: false, Bool: false}
	}

	gpk := m.GetStringD(q.replica_status_get_source_public_key(), "")
	if gpk != "" {
		instance.ReplicationGetSourcePublicKey = modeldomain.NullBool{
			Valid: true,
			Bool:  strings.EqualFold(gpk, "Yes") || gpk == "1",
		}
	} else if instance.ReplicationSourcePublicKeyPath != "" {
		// When SOURCE_PUBLIC_KEY_PATH is set, some builds omit Get_source_public_key
		// from SHOW (GET_SOURCE_PUBLIC_KEY is effectively off). Still replay
		// get_source_public_key=0 on CHANGE so options are not dropped.
		instance.ReplicationGetSourcePublicKey = modeldomain.NullBool{Valid: true, Bool: false}
	} else {
		instance.ReplicationGetSourcePublicKey = modeldomain.NullBool{Valid: false, Bool: false}
	}
}

// ReadTopologyInstance collects information on the state of a MySQL
// server and writes the result synchronously to the orchestrator
// backend.
func ReadTopologyInstance(instanceKey *InstanceKey) (*Instance, error) {
	return ReadTopologyInstanceContext(context.Background(), instanceKey)
}

func ReadTopologyInstanceContext(ctx context.Context, instanceKey *InstanceKey) (*Instance, error) {
	instance, skipped, err := ReadTopologyInstanceBufferableContext(ctx, instanceKey, false, nil)
	if skipped {
		if config.Config.Topology.Discovery.FilterLogsEnabled {
			log.Infof("Skipping discovery of %+v because its replication user matches DiscoveryIgnoreReplicationUsernameFilters", instanceKey)
		}
		instance = nil
	}
	return instance, err
}

// ReadTopologyInstances is a convenience method that calls ReadTopologyInstance
// for all the instance keys and returns a slice of Instance.
func ReadTopologyInstances(instanceKeys []InstanceKey) ([]*Instance, error) {
	instances := make([]*Instance, 0)
	for _, instanceKey := range instanceKeys {
		instance, err := ReadTopologyInstance(&instanceKey)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func RetryInstanceFunction(f func() (*Instance, error)) (instance *Instance, err error) {
	for range retryInstanceFunctionCount {
		if instance, err = f(); err == nil {
			return instance, nil
		}
	}
	return instance, err
}

// Is this an error which means that we shouldn't try going more queries for this discovery attempt?
func unrecoverableError(err error) bool {
	contains := []string{
		error1045AccessDenied,
		errorConnectionRefused,
		errorIOTimeout,
		errorNoSuchHost,
	}
	for _, k := range contains {
		if strings.Contains(err.Error(), k) {
			return true
		}
	}
	return false
}

// Check if the instance is a MaxScale binlog server (a proxy not a real
// MySQL server) and also update the resolved hostname
func (instance *Instance) checkMaxScale(database *topology.Client, latency *stopwatch.NamedStopwatch) (isMaxScale bool, resolvedHostname string, err error) {
	if config.Config.Topology.Compatibility.SkipMaxScaleCheck {
		return isMaxScale, resolvedHostname, err
	}

	latency.Start("instance")
	err = database.ReadDynamicRows("show variables like 'maxscale%'", func(m modeldomain.DynamicRow) error {
		if m.GetString("Variable_name") == "MAXSCALE_VERSION" {
			originalVersion := m.GetString("Value")
			if originalVersion == "" {
				originalVersion = m.GetString("value")
			}
			if originalVersion == "" {
				originalVersion = "0.0.0"
			}
			instance.Version = originalVersion + "-maxscale"
			instance.ServerID = 0
			instance.ServerUUID = ""
			instance.Uptime = 0
			instance.Binlog_format = "INHERIT"
			instance.ReadOnly = true
			instance.LogBinEnabled = true
			instance.LogReplicationUpdatesEnabled = true
			resolvedHostname = instance.Key.Hostname
			latency.Start("backend")
			UpdateResolvedHostname(resolvedHostname, resolvedHostname)
			latency.Stop("backend")
			isMaxScale = true
		}
		return nil
	})
	latency.Stop("instance")

	// Detect failed connection attempts and don't report the command
	// we are executing as that might be confusing.
	if err != nil {
		if strings.Contains(err.Error(), error1045AccessDenied) {
			accessDeniedCounter.Add(context.Background(), 1)
		}
		if unrecoverableError(err) {
			logReadTopologyInstanceError(&instance.Key, "", err)
		} else {
			logReadTopologyInstanceError(&instance.Key, "show variables like 'maxscale%'", err)
		}
	}

	return isMaxScale, resolvedHostname, err
}

// expectReplicationThreadsState expects both replication threads to be running, or both to be not running.
// Specifically, it looks for both to be "Yes" or for both to be "No".
func expectReplicationThreadsState(instance *Instance, instanceKey *InstanceKey, expectedState ReplicationThreadState) (expectationMet bool, err error) {
	topologyDB, err := topology.Open(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		return false, err
	}
	err = topologyDB.ReadDynamicRows(instance.QSP.show_slave_status(), func(m modeldomain.DynamicRow) error {
		ioThreadState := ReplicationThreadStateFromStatus(m.GetString(instance.QSP.slave_io_running()))
		sqlThreadState := ReplicationThreadStateFromStatus(m.GetString(instance.QSP.slave_sql_running()))

		if ioThreadState == expectedState && sqlThreadState == expectedState {
			expectationMet = true
		}
		return nil
	})
	return expectationMet, err
}

// ReadTopologyInstanceBufferable connects to a topology MySQL instance
// and collects information on the server and its replication state.
// It writes the information retrieved into orchestrator's backend.
// - writes are optionally buffered.
// - timing information can be collected for the stages performed.
func ReadTopologyInstanceBufferable(instanceKey *InstanceKey, bufferWrites bool, latency *stopwatch.NamedStopwatch) (*Instance, bool, error) {
	return ReadTopologyInstanceBufferableContext(context.Background(), instanceKey, bufferWrites, latency)
}

// ReadTopologyInstanceBufferableContext keeps discovery spans and SQL calls on the caller's context.
func ReadTopologyInstanceBufferableContext(ctx context.Context, instanceKey *InstanceKey, bufferWrites bool, latency *stopwatch.NamedStopwatch) (inst *Instance, skipped bool, err error) {
	ctx, span := observability.StartSpan(ctx, "topology.discover")
	defer func() { observability.EndSpan(span, err) }()
	defer func() {
		if r := recover(); r != nil {
			err = logReadTopologyInstanceError(instanceKey, "Unexpected, aborting", fmt.Errorf("%+v", r))
		}
	}()

	var waitGroup sync.WaitGroup
	var serverUuidWaitGroup sync.WaitGroup
	readingStartTime := time.Now()
	instance := NewInstance()
	instanceFound := false
	partialSuccess := false
	foundByShowSlaveHosts := false
	resolvedHostname := ""
	maxScaleMasterHostname := ""
	isMaxScale := false
	isMaxScale110 := false
	slaveStatusFound := false
	errorChan := make(chan error, 32)
	var resolveErr error
	instanceDiscoverySkipped := false
	masterHostnameTmp := ""
	var masterPortTmp int
	var masterKey *InstanceKey

	if !instanceKey.IsValid() {
		latency.Start("backend")
		if err := UpdateInstanceLastAttemptedCheck(instanceKey); err != nil {
			log.Errorf("ReadTopologyInstanceBufferable: %+v: %v", instanceKey, err)
		}
		latency.Stop("backend")
		return instance, instanceDiscoverySkipped, fmt.Errorf("ReadTopologyInstance will not act on invalid instance key: %+v", *instanceKey)
	}

	lastAttemptedCheckTimer := time.AfterFunc(time.Second, func() {
		go UpdateInstanceLastAttemptedCheck(instanceKey)
	})

	latency.Start("instance")
	topologyDB, err := topology.OpenDiscovery(instanceKey.Hostname, instanceKey.Port)
	if err != nil {
		latency.Stop("instance")
		DeadInstancesFilter.RegisterInstance(instanceKey)
		goto Cleanup
	}

	// Even if the instance is dead, we need its key below to update
	// the backend database's timestamps
	instance.Key = *instanceKey

	err = topologyDB.CheckConnection(ctx)
	if err != nil {
		DeadInstancesFilter.RegisterInstance(instanceKey)
		goto Cleanup
	}
	latency.Stop("instance")
	DeadInstancesFilter.UnregisterInstance(instanceKey)

	if isMaxScale, resolvedHostname, err = instance.checkMaxScale(topologyDB, latency); err != nil {
		// We do not "goto Cleanup" here, although it should be the correct flow.
		// Reason is 5.7's new security feature that requires GRANTs on performance_schema.session_variables.
		// There is a wrong decision making in this design and the migration path to 5.7 will be difficult.
		// I don't want orchestrator to put even more burden on this.
		// If the statement errors, then we are unable to determine that this is maxscale, hence assume it is not.
		// In which case there would be other queries sent to the server that are not affected by 5.7 behavior, and that will fail.

		// Certain errors are not recoverable (for this discovery process) so it's fine to go to Cleanup
		if unrecoverableError(err) {
			goto Cleanup
		}
	}

	latency.Start("instance")
	if isMaxScale {
		if strings.Contains(instance.Version, "1.1.0") {
			isMaxScale110 = true

			// Buggy buggy maxscale 1.1.0. Reported Master_Host can be corrupted.
			// Therefore we (currently) take @@hostname (which is masquerading as master host anyhow)
			err = topologyDB.ReadRowContext(ctx, "select @@hostname").Decode(&maxScaleMasterHostname)
			if err != nil {
				goto Cleanup
			}
		}
		if isMaxScale110 {
			// Only this is supported:
			topologyDB.ReadRowContext(ctx, "select @@server_id").Decode(&instance.ServerID)
		} else {
			topologyDB.ReadRowContext(ctx, "select @@global.server_id").Decode(&instance.ServerID)
			topologyDB.ReadRowContext(ctx, "select @@global.server_uuid").Decode(&instance.ServerUUID)
		}
	} else {
		// NOT MaxScale
		err = topologyDB.ReadRowContext(ctx, "select @@global.version").Decode(&instance.Version)
		if err != nil {
			goto Cleanup
		}
	}

	instance.QSP = GetQueryStringProvider(instance.Version)

	instance.ReplicationIOThreadState = ReplicationThreadStateNoThread
	instance.ReplicationSQLThreadState = ReplicationThreadStateNoThread

	// Learn if this instance should be filtered out because of replication user
	// matching DiscoveryIgnoreReplicationUsernameFilters ASAP.
	// If we got here with such an instance it means we are handling a corner case
	// When source was discovered, we checked for its replicas (below in this function)
	// If we were able to figure out the replica username, replica instance was already
	// filtered out and we don't get here with the replica.
	// However it may be that the replica is not reporting its replication username
	// to the source (e.g. DiscoverByShowSlaveHosts=true but source and replica have not
	// set show-replica-auth-info=1 and report_user set accordingly). In such a case
	// replica will not be filtered out during source examination and will go to discovery
	// queue. This is how we get here with the replica.
	err = topologyDB.ReadDynamicRowsContext(ctx, instance.QSP.show_slave_status(), func(m modeldomain.DynamicRow) error {
		user := m.GetString(instance.QSP.master_user())

		if FiltersMatchReplicationIgnoreUsername(user, config.Config.Topology.Discovery.IgnoreReplicationUsernames) {
			err = fmt.Errorf("host %+v is excluded from discovery by DiscoveryIgnoreReplicationUsernameFilters", *instanceKey)
			instanceDiscoverySkipped = true
			return err
		}

		instance.HasReplicationCredentials = (user != "")
		instance.ReplicationIOThreadState = ReplicationThreadStateFromStatus(m.GetString(instance.QSP.slave_io_running()))
		instance.ReplicationSQLThreadState = ReplicationThreadStateFromStatus(m.GetString(instance.QSP.slave_sql_running()))
		instance.ReplicationIOThreadRuning = instance.ReplicationIOThreadState.IsRunning()
		if isMaxScale110 {
			// Covering buggy MaxScale 1.1.0
			instance.ReplicationIOThreadRuning = instance.ReplicationIOThreadRuning && (m.GetString(instance.QSP.slave_io_state()) == "Binlog Dump")
		}
		instance.ReplicationSQLThreadRuning = instance.ReplicationSQLThreadState.IsRunning()
		instance.ReadBinlogCoordinates.LogFile = m.GetString(instance.QSP.master_log_file())
		instance.ReadBinlogCoordinates.LogPos = m.GetInt64(instance.QSP.read_master_log_pos())
		instance.ExecBinlogCoordinates.LogFile = m.GetString(instance.QSP.relay_master_log_file())
		instance.ExecBinlogCoordinates.LogPos = m.GetInt64(instance.QSP.relay_master_log_position())
		instance.IsDetached, _ = instance.ExecBinlogCoordinates.ExtractDetachedCoordinates()
		instance.RelaylogCoordinates.LogFile = m.GetString("Relay_Log_File")
		instance.RelaylogCoordinates.LogPos = m.GetInt64("Relay_Log_Pos")
		instance.RelaylogCoordinates.Type = RelayLog
		instance.LastSQLError = emptyQuotesRegexp.ReplaceAllString(strconv.QuoteToASCII(m.GetString("Last_SQL_Error")), "")
		instance.LastIOError = emptyQuotesRegexp.ReplaceAllString(strconv.QuoteToASCII(m.GetString("Last_IO_Error")), "")
		instance.SQLDelay = m.GetUintD("SQL_Delay", 0)
		instance.UsingOracleGTID = (m.GetIntD("Auto_Position", 0) == 1)
		instance.UsingMariaDBGTID = (m.GetStringD("Using_Gtid", "No") != "No")
		instance.MasterUUID = m.GetStringD(instance.QSP.master_uuid(), "No")
		instance.HasReplicationFilters = ((m.GetStringD("Replicate_Do_DB", "") != "") || (m.GetStringD("Replicate_Ignore_DB", "") != "") || (m.GetStringD("Replicate_Do_Table", "") != "") || (m.GetStringD("Replicate_Ignore_Table", "") != "") || (m.GetStringD("Replicate_Wild_Do_Table", "") != "") || (m.GetStringD("Replicate_Wild_Ignore_Table", "") != ""))

		// Remember master hostname:port. Once we update resolve cache below
		// we will use it to set instance's members
		masterHostnameTmp = m.GetString(instance.QSP.master_host())
		if isMaxScale110 {
			// Buggy buggy maxscale 1.1.0. Reported Master_Host can be corrupted.
			// Therefore we (currently) take @@hostname (which is masquarading as master host anyhow)
			masterHostnameTmp = maxScaleMasterHostname
		}
		masterPortTmp = m.GetInt(instance.QSP.master_port())

		instance.IsDetachedMaster = instance.MasterKey.IsDetached()
		instance.SecondsBehindMaster = modeldomain.NullInt64(m.GetNullInt64(instance.QSP.seconds_behind_master()))
		if instance.SecondsBehindMaster.Valid && instance.SecondsBehindMaster.Int64 < 0 {
			log.Warningf("Host: %+v, instance.SecondsBehindMaster < 0 [%+v], correcting to 0", instanceKey, instance.SecondsBehindMaster.Int64)
			instance.SecondsBehindMaster.Int64 = 0
		}
		// And until told otherwise:
		instance.ReplicationLagSeconds = instance.SecondsBehindMaster

		instance.AllowTLS = (m.GetString(instance.QSP.master_ssl_allowed()) == "Yes")
		readReplicationTLSStatusFromShowReplicaRow(instance, m)
		// Not breaking the flow even on error
		slaveStatusFound = true
		return nil
	})
	if err != nil {
		goto Cleanup
	}

	if !isMaxScale {
		// We begin with a few operations we can run concurrently, and which do not depend on anything
		{
			waitGroup.Go(func() {
				var dummy string
				// show global status works just as well with 5.6 & 5.7 (5.7 moves variables to performance_schema)
				err := topologyDB.ReadRowContext(ctx, "show global status like 'Uptime'").Decode(&dummy, &instance.Uptime)

				if err != nil {
					logReadTopologyInstanceError(instanceKey, "show global status like 'Uptime'", err)

					// We do not "goto Cleanup" here, although it should be the correct flow.
					// Reason is 5.7's new security feature that requires GRANTs on performance_schema.global_variables.
					// There is a wrong decisionmaking in this design and the migration path to 5.7 will be difficult.
					// I don't want orchestrator to put even more burden on this. The 'Uptime' variable is not that important
					// so as to completely fail reading a 5.7 instance.
					// This is supposed to be fixed in 5.7.9
				}
				errorChan <- err
			})
		}

		// Synchronously query for some params needed in following go routines
		var mysqlHostname, mysqlReportHost string
		err = topologyDB.ReadRowContext(ctx, "select @@global.hostname, ifnull(@@global.report_host, ''), @@global.server_id, @@global.version_comment, @@global.read_only, @@global.binlog_format, @@global.log_bin, @@global."+instance.QSP.log_slave_updates()).Decode(
			&mysqlHostname, &mysqlReportHost, &instance.ServerID, &instance.VersionComment, &instance.ReadOnly, &instance.Binlog_format, &instance.LogBinEnabled, &instance.LogReplicationUpdatesEnabled)
		if err != nil {
			goto Cleanup
		}
		partialSuccess = true // We at least managed to read something from the server.
		switch strings.ToLower(config.Config.Topology.Hostname.MySQLResolveMethod) {
		case "none":
			resolvedHostname = instance.Key.Hostname
		case "default", "hostname", "@@hostname":
			resolvedHostname = mysqlHostname
		case "report_host", "@@report_host":
			if mysqlReportHost == "" {
				err = fmt.Errorf("MySQLHostnameResolveMethod configured to use @@report_host but %+v has NULL/empty @@report_host", instanceKey)
				goto Cleanup
			}
			resolvedHostname = mysqlReportHost
		default:
			resolvedHostname = instance.Key.Hostname
		}

		if instance.LogBinEnabled {
			waitGroup.Go(func() {
				err := topologyDB.ReadDynamicRowsContext(ctx, instance.QSP.show_master_status(), func(m modeldomain.DynamicRow) error {
					var err error
					instance.SelfBinlogCoordinates.LogFile = m.GetString("File")
					instance.SelfBinlogCoordinates.LogPos = m.GetInt64("Position")
					return err
				})
				errorChan <- err
			})
		}

		{
			waitGroup.Go(func() {
				semiSyncMasterPluginLoaded := false
				semiSyncReplicaPluginLoaded := false
				instance.SemiSyncAvailable = false

				err := topologyDB.ReadDynamicRowsContext(ctx, "show global variables like 'rpl_semi_sync_%'", func(m modeldomain.DynamicRow) error {
					variableName := m.GetString("Variable_name")
					// Learn if semi-sync plugin is loaded and what is its version
					if variableName == "rpl_semi_sync_master_enabled" {
						instance.SemiSyncMasterEnabled = (m.GetString("Value") == "ON")
						semiSyncMasterPluginLoaded = true
						instance.SemiSyncMasterPluginNewVersion = false
					} else if variableName == "rpl_semi_sync_source_enabled" {
						instance.SemiSyncMasterEnabled = (m.GetString("Value") == "ON")
						semiSyncMasterPluginLoaded = true
						instance.SemiSyncMasterPluginNewVersion = true
					} else if variableName == "rpl_semi_sync_slave_enabled" {
						instance.SemiSyncReplicaEnabled = (m.GetString("Value") == "ON")
						semiSyncReplicaPluginLoaded = true
						instance.SemiSyncReplicaPluginNewVersion = false
					} else if variableName == "rpl_semi_sync_replica_enabled" {
						instance.SemiSyncReplicaEnabled = (m.GetString("Value") == "ON")
						semiSyncReplicaPluginLoaded = true
						instance.SemiSyncReplicaPluginNewVersion = true
					} else {
						// additional info
						matched, regexperr := regexp.MatchString("^rpl_semi_sync_(master|source)_timeout$", variableName)
						if regexperr != nil {
							return regexperr
						}
						if matched {
							instance.SemiSyncMasterTimeout = m.GetUint64("Value")
							return nil
						}
						matched, regexperr = regexp.MatchString("^rpl_semi_sync_(master|source)_wait_for_(slave|replica)_count$", variableName)
						if regexperr != nil {
							return regexperr
						}
						if matched {
							instance.SemiSyncMasterWaitForReplicaCount = m.GetUint("Value")
							return nil
						}
					}
					return nil
				})
				if err != nil {
					errorChan <- err
					return
				}

				instance.SemiSyncAvailable = (semiSyncMasterPluginLoaded && semiSyncReplicaPluginLoaded)
				errorChan <- err
			})
		}
		{
			waitGroup.Go(func() {
				err := topologyDB.ReadDynamicRowsContext(ctx, "show global status like 'rpl_semi_sync_%'", func(m modeldomain.DynamicRow) error {
					variableName := m.GetString("Variable_name")
					matched, regexperr := regexp.MatchString("^Rpl_semi_sync_(master|source)_status$", variableName)
					if regexperr != nil {
						return regexperr
					}
					if matched {
						instance.SemiSyncMasterStatus = (m.GetString("Value") == "ON")
						return nil
					}

					matched, regexperr = regexp.MatchString("^Rpl_semi_sync_(master|source)_clients$", variableName)
					if regexperr != nil {
						return regexperr
					}
					if matched {
						instance.SemiSyncMasterClients = m.GetUint("Value")
						return nil
					}
					matched, regexperr = regexp.MatchString("^Rpl_semi_sync_(slave|replica)_status$", variableName)
					if regexperr != nil {
						return regexperr
					}
					if matched {
						instance.SemiSyncReplicaStatus = (m.GetString("Value") == "ON")
					}
					return nil
				})
				errorChan <- err
			})
		}
		if (instance.IsOracleMySQL() || instance.IsPercona()) && !instance.IsSmallerMajorVersionByString("5.6") {
			waitGroup.Add(1)
			serverUuidWaitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				defer serverUuidWaitGroup.Done()
				var masterInfoRepositoryOnTable bool
				// Stuff only supported on Oracle MySQL >= 5.6
				// ...
				// @@gtid_mode only available in Orcale MySQL >= 5.6
				// Previous version just issued this query brute-force, but I don't like errors being issued where they shouldn't.
				_ = topologyDB.ReadRowContext(ctx, instance.QSP.master_gtid_info()).Decode(&instance.GTIDMode, &instance.ServerUUID, &instance.ExecutedGtidSet, &instance.GtidPurged, &masterInfoRepositoryOnTable, &instance.BinlogRowImage)
				if instance.GTIDMode != "" && instance.GTIDMode != "OFF" {
					instance.SupportsOracleGTID = true
				}
				if config.Config.Topology.Replication.CredentialsQuery != "" {
					instance.ReplicationCredentialsAvailable = true
				} else if masterInfoRepositoryOnTable {
					// mysql.slave_master_info table is still present in 8.4, no need for instance.QSP
					_ = topologyDB.ReadRowContext(ctx, "select count(*) > 0 and MAX(User_name) != '' from mysql.slave_master_info").Decode(&instance.ReplicationCredentialsAvailable)
				}
			}()
		}
	}
	log.Infof("Instance %+v, resolvedHostname %+v", instance.Key, resolvedHostname)
	if resolvedHostname != instance.Key.Hostname {
		latency.Start("backend")
		UpdateResolvedHostname(instance.Key.Hostname, resolvedHostname)
		latency.Stop("backend")
		instance.Key.Hostname = resolvedHostname
	}
	if instance.Key.Hostname == "" {
		err = fmt.Errorf("ReadTopologyInstance: empty hostname (%+v). Bailing out", *instanceKey)
		goto Cleanup
	}
	go ResolveHostnameIPs(instance.Key.Hostname)
	if config.Config.Topology.Classification.DataCenterPattern != "" {
		if pattern, err := regexp.Compile(config.Config.Topology.Classification.DataCenterPattern); err == nil {
			match := pattern.FindStringSubmatch(instance.Key.Hostname)
			if len(match) != 0 {
				instance.DataCenter = match[1]
			}
		}
		// This can be overridden by later invocation of DetectDataCenterQuery
	}
	if config.Config.Topology.Classification.RegionPattern != "" {
		if pattern, err := regexp.Compile(config.Config.Topology.Classification.RegionPattern); err == nil {
			match := pattern.FindStringSubmatch(instance.Key.Hostname)
			if len(match) != 0 {
				instance.Region = match[1]
			}
		}
		// This can be overridden by later invocation of DetectRegionQuery
	}
	if config.Config.Topology.Classification.PhysicalEnvironmentPattern != "" {
		if pattern, err := regexp.Compile(config.Config.Topology.Classification.PhysicalEnvironmentPattern); err == nil {
			match := pattern.FindStringSubmatch(instance.Key.Hostname)
			if len(match) != 0 {
				instance.PhysicalEnvironment = match[1]
			}
		}
		// This can be overridden by later invocation of DetectPhysicalEnvironmentQuery
	}

	if slaveStatusFound {
		// Resolve cache has been updated, so we can finalize resolving master
		masterKey, err = NewResolveInstanceKey(masterHostnameTmp, masterPortTmp)
		if err != nil {
			logReadTopologyInstanceError(instanceKey, "NewResolveInstanceKey", err)
		}
		masterKey.Hostname, resolveErr = ResolveHostname(masterKey.Hostname)
		if resolveErr != nil {
			logReadTopologyInstanceError(instanceKey, fmt.Sprintf("ResolveHostname(%q)", masterKey.Hostname), resolveErr)
		}
		instance.MasterKey = *masterKey
		instance.IsDetachedMaster = instance.MasterKey.IsDetached()
	}

	// Populate GR information for the instance in Oracle MySQL 8.0+ or Percona Server 8.0+. To do this we need to wait
	// for the Server UUID to be populated to be able to find this instance's information in
	// performance_schema.replication_group_members by comparing UUIDs. We could instead resolve the MEMBER_HOST and
	// MEMBER_PORT columns into an InstanceKey and compare those instead, but this could require external calls for
	// name resolving, whereas comparing UUIDs does not.
	serverUuidWaitGroup.Wait()
	if (instance.IsOracleMySQL() || instance.IsPercona()) && !instance.IsSmallerMajorVersionByString("8.0") {
		err = PopulateGroupReplicationInformation(instance, topologyDB)
		if err != nil {
			goto Cleanup
		}
	}
	if isMaxScale && !slaveStatusFound {
		err = fmt.Errorf("no 'SHOW SLAVE STATUS' output found for a MaxScale instance: %+v", instanceKey)
		goto Cleanup
	}

	if config.Config.Topology.Replication.LagQuery != "" && !isMaxScale {
		waitGroup.Go(func() {
			if err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Replication.LagQuery).Decode(&instance.ReplicationLagSeconds); err == nil {
				if instance.ReplicationLagSeconds.Valid && instance.ReplicationLagSeconds.Int64 < 0 {
					log.Warningf("Host: %+v, instance.SlaveLagSeconds < 0 [%+v], correcting to 0", instanceKey, instance.ReplicationLagSeconds.Int64)
					instance.ReplicationLagSeconds.Int64 = 0
				}
			} else {
				instance.ReplicationLagSeconds = instance.SecondsBehindMaster
				logReadTopologyInstanceError(instanceKey, "topology.replication.lagQuery", err)
			}
		})
	}

	instanceFound = true

	// -------------------------------------------------------------------------
	// Anything after this point does not affect the fact the instance is found.
	// No `goto Cleanup` after this point.
	// -------------------------------------------------------------------------

	// Get replicas, either by SHOW SLAVE HOSTS or via PROCESSLIST
	// MaxScale does not support PROCESSLIST, so SHOW SLAVE HOSTS is the only option
	//
	// Filtered out replicas (config.DiscoveryIgnoreReplicationUsernameFilters)
	// will be skipped, but only if source (this host) is provided with the information
	// about replica's replication user name.
	// 1. In case of DiscoverByShowSlaveHosts=true, it will be the case only if
	//    source is started with --show-replica-auth-info=1 and the replica has
	//    report_user=<replication_user>. If that's not the case, we are not able
	// 	  to filter the replica below and it will be enqueued into the discoveryQueue.
	//    Once ReadTopologyInstanceBufferable is called for such replica, we will
	//    check it's replication user name, but this needs some queries to the replica.
	//    (see this function above).
	//    To avoid this overhead, configure source and replica properly.
	// 2. In case of DiscoverByShowSlaveHosts=false, I_S.processlist table is used.
	// 	  It contains replica's replication user name, so the replica will be skipped
	//    always.
	if config.Config.Topology.Discovery.UseShowReplicaHosts || isMaxScale {
		err := topologyDB.ReadDynamicRowsContext(ctx, instance.QSP.show_slave_hosts(),
			func(m modeldomain.DynamicRow) error {
				// MaxScale 1.1 may trigger an error with this command, but
				// also we may see issues if anything on the MySQL server locks up.
				// Consequently it's important to validate the values received look
				// good prior to calling ResolveHostname()
				host := m.GetString("Host")
				port := m.GetIntD("Port", 0)
				user := m.GetString("User")
				if host == "" || port == 0 {
					if isMaxScale && host == "" && port == 0 {
						// MaxScale reports a bad response sometimes so ignore it.
						// - seen in 1.1.0 and 1.4.3.4
						return nil
					}
					// otherwise report the error to the caller
					return fmt.Errorf("ReadTopologyInstance(%+v) 'show slave hosts' returned row with <host,port>: <%v,%v>", instanceKey, host, port)
				}

				replicaKey, err := NewResolveInstanceKey(host, port)
				if err == nil && replicaKey.IsValid() {
					if !FiltersMatchInstanceKey(replicaKey, config.Config.Topology.Discovery.IgnoreReplicaHostnames) {
						if !FiltersMatchReplicationIgnoreUsername(user, config.Config.Topology.Discovery.IgnoreReplicationUsernames) {
							instance.AddReplicaKey(replicaKey)
						} else if config.Config.Topology.Discovery.FilterLogsEnabled {
							log.Infof("Ignoring replica %+v of %+v because its replication user matches DiscoveryIgnoreReplicationUsernameFilters", replicaKey, instanceKey)
						}
					}
					foundByShowSlaveHosts = true
				}
				return err
			})
		logReadTopologyInstanceError(instanceKey, "show slave hosts", err)
	}
	if !foundByShowSlaveHosts && !isMaxScale {
		// Either not configured to read SHOW SLAVE HOSTS or nothing was there.
		// Discover by information_schema.processlist
		waitGroup.Go(func() {
			err := topologyDB.ReadDynamicRowsContext(ctx, instance.QSP.select_user_host(),
				func(m modeldomain.DynamicRow) error {
					cname, resolveErr := ResolveHostname(m.GetString("slave_hostname"))
					user := m.GetString("user")
					if resolveErr != nil {
						logReadTopologyInstanceError(instanceKey, "ResolveHostname: processlist", resolveErr)
					}

					// Note that here we assume that the replica port is the same as our port
					// This is the best we can do, because there is no info about port in processlist.
					replicaKey := InstanceKey{Hostname: cname, Port: instance.Key.Port}
					if !FiltersMatchInstanceKey(&replicaKey, config.Config.Topology.Discovery.IgnoreReplicaHostnames) {
						if !FiltersMatchReplicationIgnoreUsername(user, config.Config.Topology.Discovery.IgnoreReplicationUsernames) {
							instance.AddReplicaKey(&replicaKey)
						} else if config.Config.Topology.Discovery.FilterLogsEnabled {
							log.Infof("Ignoring replica %+v of %+v because its replication user matches DiscoveryIgnoreReplicationUsernameFilters", replicaKey, instanceKey)
						}
					}
					return err
				})

			logReadTopologyInstanceError(instanceKey, "processlist", err)
		})
	}

	if instance.IsNDB() {
		// Discover by ndbinfo about MySQL Cluster SQL nodes
		waitGroup.Go(func() {
			err := topologyDB.ReadDynamicRowsContext(ctx, `
				select
					substring(service_URI,9) mysql_host
				from
					ndbinfo.processes
				where
					process_name='mysqld'
			`,
				func(m modeldomain.DynamicRow) error {
					cname, resolveErr := ResolveHostname(m.GetString("mysql_host"))
					if resolveErr != nil {
						logReadTopologyInstanceError(instanceKey, "ResolveHostname: ndbinfo", resolveErr)
					}
					replicaKey := InstanceKey{Hostname: cname, Port: instance.Key.Port}
					instance.AddReplicaKey(&replicaKey)
					return err
				})

			logReadTopologyInstanceError(instanceKey, "ndbinfo", err)
		})
	}

	if config.Config.Topology.Classification.DetectDataCenterQuery != "" && !isMaxScale {
		waitGroup.Go(func() {
			err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectDataCenterQuery).Decode(&instance.DataCenter)
			logReadTopologyInstanceError(instanceKey, "topology.classification.detectDataCenterQuery", err)
		})
	}

	if config.Config.Topology.Classification.DetectRegionQuery != "" && !isMaxScale {
		waitGroup.Go(func() {
			err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectRegionQuery).Decode(&instance.Region)
			logReadTopologyInstanceError(instanceKey, "topology.classification.detectRegionQuery", err)
		})
	}

	if config.Config.Topology.Classification.DetectPhysicalEnvironmentQuery != "" && !isMaxScale {
		waitGroup.Go(func() {
			err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectPhysicalEnvironmentQuery).Decode(&instance.PhysicalEnvironment)
			logReadTopologyInstanceError(instanceKey, "topology.classification.detectPhysicalEnvironmentQuery", err)
		})
	}

	if config.Config.Topology.Classification.DetectInstanceAliasQuery != "" && !isMaxScale {
		waitGroup.Go(func() {
			err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectInstanceAliasQuery).Decode(&instance.InstanceAlias)
			logReadTopologyInstanceError(instanceKey, "topology.classification.detectInstanceAliasQuery", err)
		})
	}

	if config.Config.Topology.Classification.DetectSemiSyncEnforcedQuery != "" && !isMaxScale {
		waitGroup.Go(func() {
			err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectSemiSyncEnforcedQuery).Decode(&instance.SemiSyncPriority)
			logReadTopologyInstanceError(instanceKey, "topology.classification.detectSemiSyncEnforcedQuery", err)
		})
	}

	{
		latency.Start("backend")
		err = ReadInstanceClusterAttributes(instance)
		latency.Stop("backend")
		logReadTopologyInstanceError(instanceKey, "ReadInstanceClusterAttributes", err)
	}

	{
		// Pseudo GTID
		// Depends on ReadInstanceClusterAttributes above
		instance.UsingPseudoGTID = false
		if config.Config.PseudoGTID.Auto {
			var err error
			instance.UsingPseudoGTID, err = isInjectedPseudoGTID(instance.ClusterName)
			log.Errore(err)
		} else if config.Config.PseudoGTID.DetectQuery != "" {
			waitGroup.Go(func() {
				if resultData, err := topologyDB.ReadResultDataContext(ctx, config.Config.PseudoGTID.DetectQuery); err == nil {
					if len(resultData) > 0 {
						if len(resultData[0]) > 0 {
							if resultData[0][0].Valid && resultData[0][0].String == "1" {
								instance.UsingPseudoGTID = true
							}
						}
					}
				} else {
					logReadTopologyInstanceError(instanceKey, "pseudoGTID.detectQuery", err)
				}
			})
		}
	}

	// First read the current PromotionRule from candidate_database_instance.
	{
		latency.Start("backend")
		err = ReadInstancePromotionRule(instance)
		latency.Stop("backend")
		logReadTopologyInstanceError(instanceKey, "ReadInstancePromotionRule", err)
	}
	// Then check if the instance wants to set a different PromotionRule.
	// We'll set it here on their behalf so there's no race between the first
	// time an instance is discovered, and setting a rule like "must_not".
	if config.Config.Topology.Classification.DetectPromotionRuleQuery != "" && !isMaxScale {
		waitGroup.Go(func() {
			var value string
			err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectPromotionRuleQuery).Decode(&value)
			logReadTopologyInstanceError(instanceKey, "topology.classification.detectPromotionRuleQuery", err)
			promotionRule, err := ParseCandidatePromotionRule(value)
			logReadTopologyInstanceError(instanceKey, "ParseCandidatePromotionRule", err)
			if err == nil {
				// We need to update candidate_database_instance.
				// We register the rule even if it hasn't changed,
				// to bump the last_suggested time.
				instance.PromotionRule = promotionRule
				err = RegisterCandidateInstance(NewCandidateDatabaseInstance(instanceKey, promotionRule).WithCurrentTime())
				logReadTopologyInstanceError(instanceKey, "RegisterCandidateInstance", err)
			}
		})
	}

	ReadClusterAliasOverride(instance)
	if !isMaxScale {
		if instance.SuggestedClusterAlias == "" {
			// Only need to do on masters
			if config.Config.Topology.Classification.DetectClusterAliasQuery != "" {
				clusterAlias := ""
				if err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectClusterAliasQuery).Decode(&clusterAlias); err != nil {
					logReadTopologyInstanceError(instanceKey, "topology.classification.detectClusterAliasQuery", err)
				} else {
					instance.SuggestedClusterAlias = clusterAlias
				}
			}
		}
		if instance.SuggestedClusterAlias == "" {
			// Not found by DetectClusterAliasQuery...
			// See if a ClusterNameToAlias configuration applies
			if clusterAlias := mappedClusterNameToAlias(instance.ClusterName); clusterAlias != "" {
				instance.SuggestedClusterAlias = clusterAlias
			}
		}
	}
	if instance.ReplicationDepth == 0 && config.Config.Topology.Classification.DetectClusterDomainQuery != "" && !isMaxScale {
		// Only need to do on masters
		domainName := ""
		if err := topologyDB.ReadRowContext(ctx, config.Config.Topology.Classification.DetectClusterDomainQuery).Decode(&domainName); err != nil {
			domainName = ""
			logReadTopologyInstanceError(instanceKey, "topology.classification.detectClusterDomainQuery", err)
		}
		if domainName != "" {
			latency.Start("backend")
			err := WriteClusterDomainName(instance.ClusterName, domainName)
			latency.Stop("backend")
			logReadTopologyInstanceError(instanceKey, "WriteClusterDomainName", err)
		}
	}

Cleanup:
	waitGroup.Wait()
	close(errorChan)
	err = func() error {
		if err != nil {
			return err
		}

		for err := range errorChan {
			if err != nil {
				return err
			}
		}
		return nil
	}()

	if instanceFound {
		if instance.IsCoMaster {
			// Take co-master into account, and avoid infinite loop
			instance.AncestryUUID = fmt.Sprintf("%s,%s", instance.MasterUUID, instance.ServerUUID)
		} else {
			instance.AncestryUUID = fmt.Sprintf("%s,%s", instance.AncestryUUID, instance.ServerUUID)
		}
		// Add replication group ancestry UUID as well. Otherwise, Orchestrator thinks there are errant GTIDs in group
		// members and its slaves, even though they are not.
		instance.AncestryUUID = fmt.Sprintf("%s,%s", instance.AncestryUUID, instance.ReplicationGroupName)
		instance.AncestryUUID = strings.Trim(instance.AncestryUUID, ",")
		if instance.ExecutedGtidSet != "" && instance.masterExecutedGtidSet != "" {
			// Compare master & replica GTID sets, but ignore the sets that present the master's UUID.
			// This is because orchestrator may pool master and replica at an inconvenient timing,
			// such that the replica may _seems_ to have more entries than the master, when in fact
			// it's just that the master's probing is stale.
			redactedExecutedGtidSet, _ := NewOracleGtidSet(instance.ExecutedGtidSet)
			for uuid := range strings.SplitSeq(instance.AncestryUUID, ",") {
				if uuid != instance.ServerUUID {
					redactedExecutedGtidSet.RemoveUUID(uuid)
				}
				if instance.IsCoMaster && uuid == instance.ServerUUID {
					// If this is a co-master, then this server is likely to show its own generated GTIDs as errant,
					// because its co-master has not applied them yet
					redactedExecutedGtidSet.RemoveUUID(uuid)
				}
			}
			// Avoid querying the database if there's no point:
			if !redactedExecutedGtidSet.IsEmpty() {
				redactedMasterExecutedGtidSet, _ := NewOracleGtidSet(instance.masterExecutedGtidSet)
				redactedMasterExecutedGtidSet.RemoveUUID(instance.MasterUUID)

				topologyDB.ReadRowContext(ctx, "select gtid_subtract(?, ?)", redactedExecutedGtidSet.String(), redactedMasterExecutedGtidSet.String()).Decode(&instance.GtidErrant)
			}
		}
	}

	latency.Stop("instance")
	readTopologyInstanceCounter.Add(context.Background(), 1)

	if instanceFound {
		instance.LastDiscoveryLatency = time.Since(readingStartTime)
		instance.IsLastCheckValid = true
		instance.IsRecentlyChecked = true
		instance.IsUpToDate = true
		latency.Start("backend")
		if bufferWrites {
			enqueueInstanceWrite(instance, instanceFound, err)
		} else {
			WriteInstance(instance, instanceFound, err)
		}
		lastAttemptedCheckTimer.Stop()
		latency.Stop("backend")
		return instance, instanceDiscoverySkipped, nil
	}

	// Something is wrong, could be network-wise. Record that we
	// tried to check the instance. last_attempted_check is also
	// updated on success by writeInstance.
	//
	// We also get here if the instance read was skipped because
	// it was filtered by DiscoveryIgnoreReplicationUsernameFilters.
	// As the configuration can be hot-reloaded, we want this instance
	// to be reported as 'not recently checked' (instance.IsRecentlyChecked)
	// rather than invalid.
	if !instanceDiscoverySkipped {
		latency.Start("backend")
		_ = UpdateInstanceLastChecked(&instance.Key, partialSuccess)
		latency.Stop("backend")
	}

	return nil, instanceDiscoverySkipped, err
}
