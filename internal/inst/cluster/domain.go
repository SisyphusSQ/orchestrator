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

package cluster

import (
	"context"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/repository/metadata"
)

// WriteClusterDomainName will write (and override) the domain name of a cluster
func WriteClusterDomainName(clusterName string, domainName string) error {
	writeFunc := func() error {
		err := metadata.WriteClusterDomainName(context.Background(), clusterName, domainName)
		return log.Errore(err)
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}

// ExpireClusterDomainName expires cluster_domain_name entries that haven't been updated recently.
func ExpireClusterDomainName() error {
	writeFunc := func() error {
		err := metadata.ExpireClusterDomainNames(context.Background(), config.Config.Topology.Hostname.ResolveExpiryMinutes)
		return log.Errore(err)
	}
	return metadata.ExecuteWrite(context.Background(), writeFunc)
}
