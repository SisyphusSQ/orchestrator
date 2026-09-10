/*
   Copyright 2016 Simon J Mudd

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

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	modeldomain "github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// RegisterCandidateInstance markes a given instance as suggested for successoring a master in the event of failover.
func RegisterCandidateInstance(candidate *CandidateDatabaseInstance) error {
	if candidate.LastSuggestedString == "" {
		candidate = candidate.WithCurrentTime()
	}
	writeFunc := func() error {
		err := metadata.RegisterCandidateInstance(context.Background(), modeldomain.CandidateInstance{
			Hostname:      candidate.Hostname,
			Port:          candidate.Port,
			PromotionRule: string(candidate.PromotionRule),
			LastSuggested: candidate.LastSuggestedString,
		})
		AuditOperation("register-candidate", candidate.Key(), string(candidate.PromotionRule))
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// ExpireCandidateInstances removes stale master candidate suggestions.
func ExpireCandidateInstances() error {
	writeFunc := func() error {
		err := metadata.ExpireCandidateInstances(context.Background(), config.Config.Topology.Candidate.ExpireMinutes)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// BulkReadCandidateDatabaseInstance returns a slice of
// CandidateDatabaseInstance converted to JSON.
/*
root@myorchestrator [orchestrator]> select * from candidate_database_instance;
+-------------------+------+---------------------+----------+----------------+
| hostname          | port | last_suggested      | priority | promotion_rule |
+-------------------+------+---------------------+----------+----------------+
| host1.example.com | 3306 | 2016-11-22 17:41:06 |        1 | prefer         |
| host2.example.com | 3306 | 2016-11-22 17:40:24 |        1 | prefer         |
+-------------------+------+---------------------+----------+----------------+
2 rows in set (0.00 sec)
*/
func BulkReadCandidateDatabaseInstance() ([]CandidateDatabaseInstance, error) {
	var candidateDatabaseInstances []CandidateDatabaseInstance

	// Read all promotion rules from the table
	rows, err := metadata.ReadCandidateInstances(context.Background(), config.Config.Topology.Candidate.ExpireMinutes)
	for _, row := range rows {
		cdi := CandidateDatabaseInstance{
			Hostname:            row.Hostname,
			Port:                row.Port,
			PromotionRule:       CandidatePromotionRule(row.PromotionRule),
			LastSuggestedString: row.LastSuggested,
			PromotionRuleExpiry: row.PromotionRuleExpiry,
		}
		candidateDatabaseInstances = append(candidateDatabaseInstances, cdi)
	}
	return candidateDatabaseInstances, err
}
