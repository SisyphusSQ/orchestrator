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
	"reflect"
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

// ToJSONString will marshal this configuration as JSON
func (cfg *Configuration) ToJSONString() string {
	b, _ := json.Marshal(cfg)
	return string(b)
}

// Config is *the* configuration instance, used globally to get configuration data
var Config = newConfiguration()
var readFileNames []string

func newConfiguration() *Configuration {
	return &Configuration{
		Observability: ObservabilityConfiguration{
			Tracing: TracingConfiguration{SampleRatio: 0.1},
		},
		Server: ServerConfiguration{
			Listen: ListenConfiguration{Address: ":3000"},
			TLS:    TLSConfiguration{ValidOUs: []string{}},
			Status: StatusConfiguration{Endpoint: DefaultStatusAPIEndpoint},
		},
		Raft:  RaftConfiguration{Bind: "127.0.0.1:10008", DefaultPort: 10008},
		MySQL: MySQLConfiguration{ConnectTimeoutSeconds: 2},
		Metadata: MetadataConfiguration{
			Type: "mysql",
			MySQL: MetadataMySQLConfiguration{
				MaxPoolConnections: 128,
				Port:               3306,
				ReadTimeoutSeconds: 30,
				MaxAllowedPacket:   -1,
			},
		},
		Topology: TopologyConfiguration{
			MySQL: TopologyMySQLConfiguration{
				UseMixedTLS:                 true,
				MaxAllowedPacket:            -1,
				TLSCacheTTLFactor:           100,
				DefaultPort:                 3306,
				DiscoveryReadTimeoutSeconds: 10,
				ReadTimeoutSeconds:          600,
			},
			Discovery: DiscoveryConfiguration{
				PollSeconds:                5,
				DeadPollSecondsFactor:      1,
				DeadPollMaxSeconds:         5 * 60,
				ReasonableCheckSeconds:     1,
				UnseenForgetHours:          240,
				MaxConcurrency:             300,
				QueueCapacity:              100000,
				Seeds:                      []string{},
				IgnoreReplicaHostnames:     []string{},
				IgnoreReplicationUsernames: []string{},
				FilterLogsEnabled:          true,
			},
			WriteBuffer:   WriteBufferConfiguration{Size: 100, FlushIntervalMilliseconds: 100},
			Compatibility: CompatibilityConfiguration{SkipMaxScaleCheck: true},
			Hostname: HostnameConfiguration{
				ResolveMethod:                  "default",
				MySQLResolveMethod:             "@@hostname",
				SkipBinlogServerUnresolveCheck: true,
				ResolveExpiryMinutes:           60,
			},
			Candidate:      CandidateConfiguration{ExpireMinutes: 60},
			Classification: ClassificationConfiguration{ClusterNameToAlias: make(map[string]string)},
			Pools:          PoolConfiguration{SupportFuzzyHostnames: true, ExpiryMinutes: 60},
			Analysis:       AnalysisConfiguration{ReduceCount: true},
			Operations:     TopologyOperationsConfiguration{BulkWaitTimeoutSeconds: 10, MaxConcurrentReplicaOperations: 5},
		},
		Authentication: AuthenticationConfiguration{
			Proxy:               ProxyAuthenticationConfiguration{UserHeader: "X-Forwarded-User"},
			Power:               AuthorizedSubjectsConfiguration{Users: []string{"*"}, Groups: []string{}},
			ConfigurationAdmins: AuthorizedSubjectsConfiguration{Users: []string{}, Groups: []string{}},
			AccessToken:         AccessTokenConfiguration{UseExpirySeconds: 60, ExpiryMinutes: 1440},
		},
		Agents: AgentsConfiguration{
			ServerPort:                ":3001",
			PollMinutes:               60,
			UnseenForgetHours:         6,
			StaleSeedFailMinutes:      60,
			SeedWaitSecondsBeforeSend: 2,
			TLS:                       AgentTLSConfiguration{ValidOUs: []string{}},
		},
		PseudoGTID: PseudoGTIDConfiguration{
			BinlogEventsChunkSize: 10000,
			SkipBinlogContaining:  []string{},
		},
		Hooks: HooksConfiguration{ShellCommand: "bash"},
		OSC:   OSCConfiguration{IgnoreHostnames: []string{}},
		Audit: AuditConfiguration{PurgeDays: 7},
		Consul: ConsulConfiguration{
			Scheme:             "http",
			HTTPTimeoutSeconds: 60,
			KV: ConsulKVConfiguration{
				Provider:             "consul",
				MaxKVsPerTransaction: ConsulKVsPerCluster,
				ClusterMasterPrefix:  "mysql/master",
			},
		},
	}
}

func (cfg *Configuration) postReadAdjustments() error {
	if err := cfg.validateTelemetry(); err != nil {
		return err
	}
	switch strings.ToLower(cfg.Authentication.Method) {
	case "", "basic", "multi", "proxy", "token":
	default:
		return fmt.Errorf("unsupported authentication.method %q", cfg.Authentication.Method)
	}
	if cfg.Metadata.MySQL.CredentialsConfigFile != "" {
		mySQLConfig := struct {
			Client struct {
				User     string
				Password string
			}
		}{}
		err := gcfg.ReadFileInto(&mySQLConfig, cfg.Metadata.MySQL.CredentialsConfigFile)
		if err != nil {
			return fmt.Errorf("parse orchestrator credentials file %s: %w", cfg.Metadata.MySQL.CredentialsConfigFile, err)
		}
		log.Debugf("Parsed orchestrator credentials from %s", cfg.Metadata.MySQL.CredentialsConfigFile)
		cfg.Metadata.MySQL.User = mySQLConfig.Client.User
		cfg.Metadata.MySQL.Password = mySQLConfig.Client.Password
	}
	{
		// We accept password in the form "${SOME_ENV_VARIABLE}" in which case we pull
		// the given variable from os env
		submatch := envVariableRegexp.FindStringSubmatch(cfg.Metadata.MySQL.Password)
		if len(submatch) > 1 {
			cfg.Metadata.MySQL.Password = os.Getenv(submatch[1])
		}
	}
	if cfg.Topology.MySQL.CredentialsConfigFile != "" {
		mySQLConfig := struct {
			Client struct {
				User     string
				Password string
			}
		}{}
		err := gcfg.ReadFileInto(&mySQLConfig, cfg.Topology.MySQL.CredentialsConfigFile)
		if err != nil {
			return fmt.Errorf("parse topology credentials file %s: %w", cfg.Topology.MySQL.CredentialsConfigFile, err)
		}
		log.Debugf("Parsed topology credentials from %s", cfg.Topology.MySQL.CredentialsConfigFile)
		cfg.Topology.MySQL.User = mySQLConfig.Client.User
		cfg.Topology.MySQL.Password = mySQLConfig.Client.Password
	}
	{
		// We accept password in the form "${SOME_ENV_VARIABLE}" in which case we pull
		// the given variable from os env
		submatch := envVariableRegexp.FindStringSubmatch(cfg.Topology.MySQL.Password)
		if len(submatch) > 1 {
			cfg.Topology.MySQL.Password = os.Getenv(submatch[1])
		}
	}

	if cfg.Server.URLPrefix != "" {
		// Ensure the prefix starts with "/" and has no trailing one.
		cfg.Server.URLPrefix = strings.TrimLeft(cfg.Server.URLPrefix, "/")
		cfg.Server.URLPrefix = strings.TrimRight(cfg.Server.URLPrefix, "/")
		cfg.Server.URLPrefix = "/" + cfg.Server.URLPrefix
	}

	if cfg.IsSQLite() && cfg.Metadata.SQLite.DataFile == "" {
		return fmt.Errorf("metadata.sqlite.dataFile must be set when metadata.type is sqlite3")
	}
	if cfg.IsSQLite() {
		//		this.Topology.Hostname.ResolveMethod = "none"
	}
	if cfg.Consul.KV.ClusterMasterPrefix != "/" {
		// "/" remains "/"
		// "prefix" turns to "prefix/"
		// "some/prefix///" turns to "some/prefix/"
		cfg.Consul.KV.ClusterMasterPrefix = strings.TrimRight(cfg.Consul.KV.ClusterMasterPrefix, "/")
		cfg.Consul.KV.ClusterMasterPrefix = fmt.Sprintf("%s/", cfg.Consul.KV.ClusterMasterPrefix)
	}
	if cfg.PseudoGTID.Auto {
		cfg.PseudoGTID.Pattern = "drop view if exists `_pseudo_gtid_`"
		cfg.PseudoGTID.PatternIsFixedSubstring = true
		cfg.PseudoGTID.MonotonicHint = "asc:"
		cfg.PseudoGTID.DetectQuery = SelectTrueQuery
	}
	if cfg.Server.HTTPAdvertise != "" {
		u, err := url.Parse(cfg.Server.HTTPAdvertise)
		if err != nil {
			return fmt.Errorf("failed parsing server.httpAdvertise %s: %s", cfg.Server.HTTPAdvertise, err.Error())
		}
		if u.Scheme == "" {
			return fmt.Errorf("server.httpAdvertise must include scheme (http:// or https://)")
		}
		if u.Hostname() == "" {
			return fmt.Errorf("server.httpAdvertise must include host name")
		}
		if u.Port() == "" {
			return fmt.Errorf("server.httpAdvertise must include port number")
		}
		if u.Path != "" {
			return fmt.Errorf("server.httpAdvertise must not specify a path")
		}
		if cfg.Topology.WriteBuffer.Size <= 0 {
			cfg.Topology.WriteBuffer.Enabled = false
		}
	}
	if cfg.Consul.KV.MaxKVsPerTransaction < ConsulKVsPerCluster {
		cfg.Consul.KV.MaxKVsPerTransaction = ConsulKVsPerCluster
	} else if cfg.Consul.KV.MaxKVsPerTransaction > ConsulMaxTransactionOps {
		cfg.Consul.KV.MaxKVsPerTransaction = ConsulMaxTransactionOps
	}
	if err := cfg.normalizeAndValidateConsul(); err != nil {
		return err
	}
	if cfg.Topology.Discovery.DeadPollSecondsFactor < 1 {
		return fmt.Errorf("topology.discovery.deadPollSecondsFactor cannot be smaller than 1")
	}

	if cfg.Topology.Discovery.DeadPollMaxSeconds < cfg.Topology.Discovery.PollSeconds {
		return fmt.Errorf("topology.discovery.deadPollMaxSeconds cannot be smaller than topology.discovery.pollSeconds")
	}
	return nil
}

func (cfg *Configuration) IsSQLite() bool {
	return strings.Contains(cfg.Metadata.Type, "sqlite")
}

func (cfg *Configuration) IsMySQL() bool {
	return cfg.Metadata.Type == "mysql" || cfg.Metadata.Type == ""
}

// ValidateRaft validates the mandatory server runtime; offline admin commands do not start Raft.
func (cfg *Configuration) ValidateRaft() error {
	if cfg.Raft.DataDir == "" {
		return fmt.Errorf("raft.dataDir must be defined for server startup")
	}
	cfg.Raft.NodeID = strings.TrimSpace(cfg.Raft.NodeID)
	if cfg.Raft.NodeID == "" {
		return fmt.Errorf("raft.nodeID must be defined for server startup")
	}
	if strings.ContainsAny(cfg.Raft.NodeID, " \t\r\n") {
		return fmt.Errorf("raft.nodeID must not contain whitespace")
	}
	if cfg.Raft.Bind == "" {
		return fmt.Errorf("raft.bind must be defined for server startup")
	}
	normalizedBind, err := NormalizeRaftAddress(cfg.Raft.Bind, cfg.Raft.DefaultPort)
	if err != nil {
		return fmt.Errorf("raft.bind is invalid: %w", err)
	}
	cfg.Raft.Bind = normalizedBind
	if cfg.Raft.Advertise == "" {
		cfg.Raft.Advertise = cfg.Raft.Bind
	} else {
		normalizedAdvertise, err := NormalizeRaftAddress(cfg.Raft.Advertise, cfg.Raft.DefaultPort)
		if err != nil {
			return fmt.Errorf("raft.advertise is invalid: %w", err)
		}
		cfg.Raft.Advertise = normalizedAdvertise
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
	if err := validateConfigurationKeys(jsonDocument, reflect.TypeFor[Configuration]()); err != nil {
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

func validateConfigurationKeys(document []byte, configurationType reflect.Type) error {
	if configurationType.Kind() != reflect.Struct {
		return nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(document, &object); err != nil {
		// The regular decoder below owns type and syntax errors. This validator only
		// tightens object key matching, which encoding/json otherwise treats as
		// case-insensitive.
		return nil
	}

	fields := make(map[string]reflect.StructField, configurationType.NumField())
	for field := range configurationType.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" {
			name = field.Name
		}
		if name != "-" {
			fields[name] = field
		}
	}

	for name, value := range object {
		field, ok := fields[name]
		if !ok {
			return fmt.Errorf("json: unknown field %q", name)
		}
		fieldType := field.Type
		for fieldType.Kind() == reflect.Pointer {
			fieldType = fieldType.Elem()
		}
		if fieldType.Kind() == reflect.Struct {
			if err := validateConfigurationKeys(value, fieldType); err != nil {
				return err
			}
		}
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
		if candidate.Raft.NodeID != Config.Raft.NodeID || candidate.Raft.DataDir != Config.Raft.DataDir ||
			candidate.Raft.Bind != Config.Raft.Bind || candidate.Raft.Advertise != Config.Raft.Advertise ||
			candidate.Server.HTTPAdvertise != Config.Server.HTTPAdvertise || candidate.Server.Listen.Address != Config.Server.Listen.Address ||
			candidate.Raft.DefaultPort != Config.Raft.DefaultPort {
			return Config, fmt.Errorf("raft identity and address changes require a process restart")
		}
	}
	if telemetryConfigurationLocked && (candidate.Observability.Tracing.Endpoint != Config.Observability.Tracing.Endpoint || candidate.Observability.Tracing.SampleRatio != Config.Observability.Tracing.SampleRatio) {
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
