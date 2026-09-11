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

package equivalence

import (
	"context"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

func WriteMasterPositionEquivalence(master1Key *instmodel.InstanceKey, master1BinlogCoordinates *instmodel.BinlogCoordinates,
	master2Key *instmodel.InstanceKey, master2BinlogCoordinates *instmodel.BinlogCoordinates) error {
	if master1Key.Equals(master2Key) {
		// Not interesting
		return nil
	}
	writeFunc := func() error {
		err := metadata.WriteMasterPositionEquivalence(context.Background(),
			modeldomain.EquivalentCoordinates{Hostname: master1Key.Hostname, Port: master1Key.Port, BinlogFile: master1BinlogCoordinates.LogFile, BinlogPos: master1BinlogCoordinates.LogPos},
			modeldomain.EquivalentCoordinates{Hostname: master2Key.Hostname, Port: master2Key.Port, BinlogFile: master2BinlogCoordinates.LogFile, BinlogPos: master2BinlogCoordinates.LogPos},
		)
		return log.Errore(err)
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}

func GetEquivalentMasterCoordinates(instanceCoordinates *InstanceBinlogCoordinates) (result []*InstanceBinlogCoordinates, err error) {
	rows, err := metadata.ReadEquivalentMasterCoordinates(context.Background(), modeldomain.EquivalentCoordinates{
		Hostname:   instanceCoordinates.Key.Hostname,
		Port:       instanceCoordinates.Key.Port,
		BinlogFile: instanceCoordinates.Coordinates.LogFile,
		BinlogPos:  instanceCoordinates.Coordinates.LogPos,
	})
	for _, row := range rows {
		equivalentCoordinates := InstanceBinlogCoordinates{}
		equivalentCoordinates.Key.Hostname = row.Hostname
		equivalentCoordinates.Key.Port = row.Port
		equivalentCoordinates.Coordinates.LogFile = row.BinlogFile
		equivalentCoordinates.Coordinates.LogPos = row.BinlogPos
		result = append(result, &equivalentCoordinates)
	}

	if err != nil {
		return nil, err
	}

	return result, nil
}

func GetEquivalentBinlogCoordinatesFor(instanceCoordinates *InstanceBinlogCoordinates, belowKey *instmodel.InstanceKey) (*instmodel.BinlogCoordinates, error) {
	possibleCoordinates, err := GetEquivalentMasterCoordinates(instanceCoordinates)
	if err != nil {
		return nil, err
	}
	for _, instanceCoordinates := range possibleCoordinates {
		if instanceCoordinates.Key.Equals(belowKey) {
			return &instanceCoordinates.Coordinates, nil
		}
	}
	return nil, nil
}

// ExpireMasterPositionEquivalence expires old master_position_equivalence
func ExpireMasterPositionEquivalence() error {
	writeFunc := func() error {
		err := metadata.ExpireMasterPositionEquivalence(context.Background(), config.Config.Topology.Discovery.UnseenForgetHours)
		return log.Errore(err)
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}
