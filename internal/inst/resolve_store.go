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

package inst

import (
	"context"

	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/repository/metadata"

	"github.com/openark/orchestrator/internal/observability"
)

var writeResolvedHostnameCounter = observability.NewCounter("orchestrator_resolve_write_resolved_total", "resolve.write_resolved events")
var writeUnresolvedHostnameCounter = observability.NewCounter("orchestrator_resolve_write_unresolved_total", "resolve.write_unresolved events")
var readResolvedHostnameCounter = observability.NewCounter("orchestrator_resolve_read_resolved_total", "resolve.read_resolved events")
var readUnresolvedHostnameCounter = observability.NewCounter("orchestrator_resolve_read_unresolved_total", "resolve.read_unresolved events")
var readAllResolvedHostnamesCounter = observability.NewCounter("orchestrator_resolve_read_resolved_all_total", "resolve.read_resolved_all events")

// WriteResolvedHostname stores a hostname and the resolved hostname to backend database
func WriteResolvedHostname(hostname string, resolvedHostname string) error {
	writeFunc := func() error {
		err := metadata.WriteResolvedHostname(context.Background(), hostname, resolvedHostname)
		if err != nil {
			return log.Errore(err)
		}
		writeResolvedHostnameCounter.Add(context.Background(), 1)
		return nil
	}
	return ExecDBWriteFunc(writeFunc)
}

// ReadResolvedHostname returns the resolved hostname given a hostname, or empty if not exists
func ReadResolvedHostname(hostname string) (string, error) {
	var resolvedHostname string = ""

	rows, err := metadata.ReadResolvedHostname(context.Background(), hostname)
	if err == nil && len(rows) > 0 {
		resolvedHostname = rows[0].ResolvedHostname
	}
	readResolvedHostnameCounter.Add(context.Background(), 1)

	if err != nil {
		log.Errore(err)
	}
	return resolvedHostname, err
}

func ReadAllHostnameResolves() ([]HostnameResolve, error) {
	rows, err := metadata.ReadAllHostnameResolves(context.Background())
	res := make([]HostnameResolve, 0, len(rows))
	for _, row := range rows {
		res = append(res, HostnameResolve{hostname: row.Hostname, resolvedHostname: row.ResolvedHostname})
	}
	readAllResolvedHostnamesCounter.Add(context.Background(), 1)

	if err != nil {
		log.Errore(err)
	}
	return res, err
}

// ReadAllHostnameUnresolves returns the content of the hostname_unresolve table
func ReadAllHostnameUnresolves() ([]HostnameUnresolve, error) {
	rows, err := metadata.ReadAllHostnameUnresolves(context.Background())
	unres := make([]HostnameUnresolve, 0, len(rows))
	for _, row := range rows {
		unres = append(unres, HostnameUnresolve{hostname: row.Hostname, unresolvedHostname: row.UnresolvedHostname})
	}

	return unres, log.Errore(err)
}

// ReadAllHostnameUnresolves returns the content of the hostname_unresolve table
func ReadAllHostnameUnresolvesRegistrations() (registrations []HostnameRegistration, err error) {
	unresolves, err := ReadAllHostnameUnresolves()
	if err != nil {
		return registrations, err
	}
	for _, unresolve := range unresolves {
		registration := NewHostnameRegistration(&InstanceKey{Hostname: unresolve.hostname}, unresolve.unresolvedHostname)
		registrations = append(registrations, *registration)
	}
	return registrations, nil
}

// readUnresolvedHostname reverse-reads hostname resolve. It returns a hostname which matches given pattern and resovles to resolvedHostname,
// or, in the event no such hostname is found, the given resolvedHostname, unchanged.
func readUnresolvedHostname(hostname string) (string, error) {
	unresolvedHostname := hostname
	rows, err := metadata.ReadUnresolvedHostname(context.Background(), hostname)
	if err == nil && len(rows) > 0 {
		unresolvedHostname = rows[0].UnresolvedHostname
	}
	readUnresolvedHostnameCounter.Add(context.Background(), 1)

	if err != nil {
		log.Errore(err)
	}
	return unresolvedHostname, err
}

// WriteHostnameUnresolve upserts an entry in hostname_unresolve
func WriteHostnameUnresolve(instanceKey *InstanceKey, unresolvedHostname string) error {
	writeFunc := func() error {
		err := metadata.WriteHostnameUnresolve(context.Background(), instanceKey.Hostname, unresolvedHostname)
		if err != nil {
			return log.Errore(err)
		}
		writeUnresolvedHostnameCounter.Add(context.Background(), 1)
		return nil
	}
	return ExecDBWriteFunc(writeFunc)
}

// DeleteHostnameUnresolve removes an unresolve entry
func DeleteHostnameUnresolve(instanceKey *InstanceKey) error {
	writeFunc := func() error {
		err := metadata.DeleteHostnameUnresolve(context.Background(), instanceKey.Hostname)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// ExpireHostnameUnresolve expires hostname_unresolve entries that haven't been updated recently.
func ExpireHostnameUnresolve() error {
	writeFunc := func() error {
		err := metadata.ExpireHostnameUnresolve(context.Background(), config.Config.Topology.Hostname.ResolveExpiryMinutes)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// ForgetExpiredHostnameResolves
func ForgetExpiredHostnameResolves() error {
	return metadata.ForgetExpiredHostnameResolves(
		context.Background(), 2*config.Config.Topology.Hostname.ResolveExpiryMinutes,
	)
}

// DeleteInvalidHostnameResolves removes invalid resolves. At this time these are:
// - infinite loop resolves (A->B and B->A), remove earlier mapping
func DeleteInvalidHostnameResolves() error {
	rows, err := metadata.ReadInvalidHostnameResolves(context.Background())
	if err != nil {
		return err
	}
	invalidHostnames := rows

	for _, invalidHostname := range invalidHostnames {
		err = metadata.DeleteResolvedHostname(context.Background(), invalidHostname)
		log.Errore(err)
	}
	return err
}

// deleteHostnameResolves compeltely erases the database cache
func deleteHostnameResolves() error {
	return metadata.DeleteAllHostnameResolves(context.Background())
}

// writeHostnameIPs stroes an ipv4 and ipv6 associated witha hostname, if available
func writeHostnameIPs(hostname string, ipv4String string, ipv6String string) error {
	writeFunc := func() error {
		err := metadata.WriteHostnameIPs(context.Background(), hostname, ipv4String, ipv6String)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// readUnresolvedHostname reverse-reads hostname resolve. It returns a hostname which matches given pattern and resovles to resolvedHostname,
// or, in the event no such hostname is found, the given resolvedHostname, unchanged.
func readHostnameIPs(hostname string) (ipv4 string, ipv6 string, err error) {
	rows, err := metadata.ReadHostnameIPs(context.Background(), hostname)
	if err == nil && len(rows) > 0 {
		ipv4 = rows[0].IPv4
		ipv6 = rows[0].IPv6
	}
	return ipv4, ipv6, log.Errore(err)
}
