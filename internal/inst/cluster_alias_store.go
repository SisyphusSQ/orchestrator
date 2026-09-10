/*
   Copyright 2015 Shlomi Noach, courtesy Booking.com

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

package inst

import (
	"context"
	"fmt"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

func firstClusterAlias(rows []modeldomain.ClusterAlias) string {
	if len(rows) == 0 {
		return ""
	}
	return rows[0].Alias
}

func IsSQLite() bool {
	return config.Config.IsSQLite()
}

// ReadClusterNameByAlias
func ReadClusterNameByAlias(alias string) (clusterName string, err error) {
	rows, err := metadata.ReadClusterNameByAlias(context.Background(), alias)
	if err == nil && len(rows) > 0 {
		clusterName = rows[0]
	}
	if err != nil {
		return "", err
	}
	if clusterName == "" {
		err = fmt.Errorf("No cluster found for alias %s", alias)
	}
	return clusterName, err
}

// DeduceClusterName attempts to resolve a cluster name given a name or alias.
// If unsuccessful to match by alias, the function returns the same given string
func DeduceClusterName(nameOrAlias string) (clusterName string, err error) {
	if nameOrAlias == "" {
		return "", fmt.Errorf("empty cluster name")
	}
	if name, err := ReadClusterNameByAlias(nameOrAlias); err == nil {
		return name, nil
	}
	return nameOrAlias, nil
}

// ReadAliasByClusterName returns the cluster alias for the given cluster name,
// or the cluster name itself if not explicit alias found
func ReadAliasByClusterName(clusterName string) (alias string, err error) {
	alias = clusterName // default return value
	rows, err := metadata.ReadClusterAlias(context.Background(), clusterName)
	if err == nil {
		alias = firstClusterAlias(rows)
	}
	return alias, err
}

// WriteClusterAlias will write (and override) a single cluster name mapping
func writeClusterAlias(clusterName string, alias string) error {
	writeFunc := func() error {
		err := metadata.WriteClusterAlias(context.Background(), clusterName, alias)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// writeClusterAliasManualOverride will write (and override) a single cluster name mapping
func writeClusterAliasManualOverride(clusterName string, alias string) error {
	writeFunc := func() error {
		err := metadata.WriteClusterAliasOverride(context.Background(), clusterName, alias)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// Original, safe approach, which uses REPLACE INTO
func updateClusterAliasesUsingReplace() error {
	return metadata.UpdateClusterAliasesUsingReplace(context.Background(), DowntimeLostInRecoveryMessage)
}

// Optimized approach using INSERT INTO ... ON DUPLICATE KEY UPDATE
// While this approach is much faster and works in most cases, it is not
// guaranteed to be working in every case.
// cluster_alias table has two unique indexes:
// 1. primary on cluster_name column
// 2. alias_uidx on alias colum
//
// The data which is going to be inserted originates from database_instance
// table, in particular the following columns:
//  1. `cluster_name` varchar(128) NOT NULL
//  2. `suggested_cluster_alias` varchar(128) CHARACTER SET utf8mb4 COLLATE
//     utf8mb4_bin NOT NULL
//
// So it is possible to end up in the following situation when we use this
// approach:
// create table t1 (a int primary key, b int, unique key (b));
// insert into t1 values (0, 1);
// insert into t1 values (1, 2);
// insert into t1 values (0, 2) on duplicate key update a=0, b=2;
// ERROR 1062 (23000): Duplicate entry '2' for key 't1.b'
func updateClusterAliasesUsingInsert() error {
	return metadata.UpdateClusterAliasesUsingInsert(context.Background(), DowntimeLostInRecoveryMessage)
}

// UpdateClusterAliases writes down the cluster_alias table based on information
// gained from database_instance
func UpdateClusterAliases() error {
	writeFunc := func() error {
		var err error
		if IsSQLite() {
			// Sql lite backend
			err = updateClusterAliasesUsingReplace()
		} else {
			// MySQL backend (Orchestrator supports only SQLite and MySQL backends)
			// INSERT ON DUPLICATE KEY UPDATE is more performant than REPLACE in MySQL
			err = updateClusterAliasesUsingInsert()
			if err != nil {
				// Fallback to the original, safe implementation
				err = updateClusterAliasesUsingReplace()
			}
		}
		return log.Errore(err)
	}
	if err := ExecDBWriteFunc(writeFunc); err != nil {
		return err
	}
	writeFunc = func() error {
		// Handling the case where no cluster alias exists: we write a dummy alias in the form of the real cluster name.
		err := metadata.WriteMissingClusterAliases(context.Background())
		return log.Errore(err)
	}
	if err := ExecDBWriteFunc(writeFunc); err != nil {
		return err
	}
	return nil
}

// ForgetLongUnseenClusterAliases will remove entries of cluster_aliases that have long since been last seen.
// This function is compatible with ForgetLongUnseenInstances
func ForgetLongUnseenClusterAliases() error {
	rows, err := metadata.ForgetLongUnseenClusterAliases(
		context.Background(), config.Config.Topology.Discovery.UnseenForgetHours,
	)
	if err != nil {
		return log.Errore(err)
	}
	AuditOperation("forget-clustr-aliases", nil, fmt.Sprintf("Forgotten aliases: %d", rows))
	return err
}

// ReplaceAliasClusterName replaces alis mapping of one cluster name onto a new cluster name.
// Used in topology failover/recovery
func ReplaceAliasClusterName(oldClusterName string, newClusterName string) (err error) {
	{
		writeFunc := func() error {
			err := metadata.ReplaceClusterAliasName(context.Background(), oldClusterName, newClusterName)
			return log.Errore(err)
		}
		err = ExecDBWriteFunc(writeFunc)
	}
	{
		writeFunc := func() error {
			err := metadata.ReplaceClusterAliasOverrideName(context.Background(), oldClusterName, newClusterName)
			return log.Errore(err)
		}
		if ferr := ExecDBWriteFunc(writeFunc); ferr != nil {
			err = ferr
		}
	}
	return err
}

// ReadUnambiguousSuggestedClusterAliases reads potential master hostname:port who have suggested cluster aliases,
// where no one else shares said suggested cluster alias. Such hostname:port are likely true owners
// of the alias.
func ReadUnambiguousSuggestedClusterAliases() (result map[string]InstanceKey, err error) {
	result = map[string]InstanceKey{}

	rows, err := metadata.ReadUnambiguousSuggestedClusterAliases(context.Background())
	for _, row := range rows {
		result[row.SuggestedAlias] = InstanceKey{Hostname: row.Hostname, Port: row.Port}
	}
	return result, err
}
