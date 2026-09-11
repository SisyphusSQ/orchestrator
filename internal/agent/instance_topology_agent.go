/*
   Copyright 2017 GitHub Inc.

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

package agent

import (
	"encoding/json"
	"fmt"

	"github.com/openark/orchestrator/internal/golib/log"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instrelocation "github.com/openark/orchestrator/internal/inst/change/relocation"
	instreplication "github.com/openark/orchestrator/internal/inst/change/replication"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
)

func SyncReplicaRelayLogs(instance, otherInstance *instmodel.Instance) (*instmodel.Instance, error) {
	var err error
	var found bool
	var nextCoordinates *instmodel.BinlogCoordinates
	var content string
	onResponse := func(contentBytes []byte) {
		json.Unmarshal(contentBytes, &content)
	}
	log.Debugf("SyncReplicaRelayLogs: stopping replication")

	if !instance.ReplicationThreadsStopped() {
		return instance, log.Errorf("SyncReplicaRelayLogs: replication on %+v must not run", instance.Key)
	}
	if !otherInstance.ReplicationThreadsStopped() {
		return instance, log.Errorf("SyncReplicaRelayLogs: replication on %+v must not run", otherInstance.Key)
	}

	log.Debugf("SyncReplicaRelayLogs: correlating coordinates of %+v on %+v", instance.Key, otherInstance.Key)
	_, _, nextCoordinates, found, err = instrelocation.CorrelateRelaylogCoordinates(instance, nil, otherInstance)
	if err != nil {
		goto Cleanup
	}
	if !found {
		goto Cleanup
	}
	log.Debugf("SyncReplicaRelayLogs: correlated next-coordinates are %+v", *nextCoordinates)

	InitHttpClient()
	if _, err := RelaylogContentsTail(otherInstance.Key.Hostname, nextCoordinates, &onResponse); err != nil {
		goto Cleanup
	}
	log.Debugf("SyncReplicaRelayLogs: got content (%d bytes)", len(content))

	if _, err := ApplyRelaylogContents(instance.Key.Hostname, content); err != nil {
		goto Cleanup
	}
	log.Debugf("SyncReplicaRelayLogs: applied content (%d bytes)", len(content))

	instance, err = instreplication.ChangeMasterTo(&instance.Key, &otherInstance.MasterKey, &otherInstance.ExecBinlogCoordinates, false, modeldomain.GTIDHintNeutral)
	if err != nil {
		goto Cleanup
	}

Cleanup:
	if err != nil {
		return instance, log.Errore(err)
	}
	// and we're done (pending deferred functions)
	instaudit.AuditOperation("align-via-relaylogs", &instance.Key, fmt.Sprintf("aligned %+v by relaylogs from %+v", instance.Key, otherInstance.Key))

	return instance, err
}
