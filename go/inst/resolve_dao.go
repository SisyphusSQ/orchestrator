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

	"github.com/openark/golib/log"
	"github.com/openark/orchestrator/go/config"
	"github.com/openark/orchestrator/go/db"
	"github.com/rcrowley/go-metrics"
)

var writeResolvedHostnameCounter = metrics.NewCounter()
var writeUnresolvedHostnameCounter = metrics.NewCounter()
var readResolvedHostnameCounter = metrics.NewCounter()
var readUnresolvedHostnameCounter = metrics.NewCounter()
var readAllResolvedHostnamesCounter = metrics.NewCounter()

func init() {
	metrics.Register("resolve.write_resolved", writeResolvedHostnameCounter)
	metrics.Register("resolve.write_unresolved", writeUnresolvedHostnameCounter)
	metrics.Register("resolve.read_resolved", readResolvedHostnameCounter)
	metrics.Register("resolve.read_unresolved", readUnresolvedHostnameCounter)
	metrics.Register("resolve.read_resolved_all", readAllResolvedHostnamesCounter)
}

// WriteResolvedHostname stores a hostname and the resolved hostname to backend database
func WriteResolvedHostname(hostname string, resolvedHostname string) error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
			insert into
					hostname_resolve (hostname, resolved_hostname, resolved_timestamp)
				values
					(?, ?, NOW())
				on duplicate key update
					resolved_hostname = VALUES(resolved_hostname),
					resolved_timestamp = VALUES(resolved_timestamp)
			`,
			hostname,
			resolvedHostname)
		if err != nil {
			return log.Errore(err)
		}
		if hostname != resolvedHostname {
			// history is only interesting when there's actually something to resolve...
			_, err = db.ExecOrchestrator(`
			insert into
					hostname_resolve_history (hostname, resolved_hostname, resolved_timestamp)
				values
					(?, ?, NOW())
				on duplicate key update
					hostname=values(hostname),
					resolved_timestamp=values(resolved_timestamp)
			`,
				hostname,
				resolvedHostname)
		}
		writeResolvedHostnameCounter.Inc(1)
		return nil
	}
	return ExecDBWriteFunc(writeFunc)
}

// ReadResolvedHostname returns the resolved hostname given a hostname, or empty if not exists
func ReadResolvedHostname(hostname string) (string, error) {
	var resolvedHostname string = ""

	query := `
		select
			resolved_hostname
		from
			hostname_resolve
		where
			hostname = ?
		`

	type resolvedHostnameRow struct {
		ResolvedHostname string `gorm:"column:resolved_hostname"`
	}
	rows, err := db.QueryOrchestratorRows[resolvedHostnameRow](context.Background(), query, hostname)
	if err == nil && len(rows) > 0 {
		resolvedHostname = rows[0].ResolvedHostname
	}
	readResolvedHostnameCounter.Inc(1)

	if err != nil {
		log.Errore(err)
	}
	return resolvedHostname, err
}

func ReadAllHostnameResolves() ([]HostnameResolve, error) {
	res := []HostnameResolve{}
	query := `
		select
			hostname,
			resolved_hostname
		from
			hostname_resolve
		`
	type hostnameResolveRow struct {
		Hostname         string `gorm:"column:hostname"`
		ResolvedHostname string `gorm:"column:resolved_hostname"`
	}
	rows, err := db.QueryOrchestratorRows[hostnameResolveRow](context.Background(), query)
	for _, row := range rows {
		res = append(res, HostnameResolve{hostname: row.Hostname, resolvedHostname: row.ResolvedHostname})
	}
	readAllResolvedHostnamesCounter.Inc(1)

	if err != nil {
		log.Errore(err)
	}
	return res, err
}

// ReadAllHostnameUnresolves returns the content of the hostname_unresolve table
func ReadAllHostnameUnresolves() ([]HostnameUnresolve, error) {
	unres := []HostnameUnresolve{}
	query := `
		select
			hostname,
			unresolved_hostname
		from
			hostname_unresolve
		`
	type hostnameUnresolveRow struct {
		Hostname           string `gorm:"column:hostname"`
		UnresolvedHostname string `gorm:"column:unresolved_hostname"`
	}
	rows, err := db.QueryOrchestratorRows[hostnameUnresolveRow](context.Background(), query)
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
	type hostnameUnresolveRow struct {
		UnresolvedHostname string `gorm:"column:unresolved_hostname"`
	}
	unresolvedHostname := hostname

	query := `
	   		select
	   			unresolved_hostname
	   		from
	   			hostname_unresolve
	   		where
	   			hostname = ?
	   		`

	rows, err := db.QueryOrchestratorRows[hostnameUnresolveRow](context.Background(), query, hostname)
	if err == nil && len(rows) > 0 {
		unresolvedHostname = rows[0].UnresolvedHostname
	}
	readUnresolvedHostnameCounter.Inc(1)

	if err != nil {
		log.Errore(err)
	}
	return unresolvedHostname, err
}

// readMissingHostnamesToResolve gets those (unresolved, e.g. VIP) hostnames that *should* be present in
// the hostname_resolve table, but aren't.
func readMissingKeysToResolve() (result InstanceKeyMap, err error) {
	query := `
   		select
   				hostname_unresolve.unresolved_hostname,
   				database_instance.port
   			from
   				database_instance
   				join hostname_unresolve on (database_instance.hostname = hostname_unresolve.hostname)
   				left join hostname_resolve on (database_instance.hostname = hostname_resolve.resolved_hostname)
   			where
   				hostname_resolve.hostname is null
	   		`

	type missingResolveRow struct {
		UnresolvedHostname string `gorm:"column:unresolved_hostname"`
		Port               int    `gorm:"column:port"`
	}
	rows, err := db.QueryOrchestratorRows[missingResolveRow](context.Background(), query)
	for _, row := range rows {
		instanceKey := InstanceKey{Hostname: row.UnresolvedHostname, Port: row.Port}
		result.AddKey(instanceKey)
	}

	if err != nil {
		log.Errore(err)
	}
	return result, err
}

// WriteHostnameUnresolve upserts an entry in hostname_unresolve
func WriteHostnameUnresolve(instanceKey *InstanceKey, unresolvedHostname string) error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
        	insert into hostname_unresolve (
        		hostname,
        		unresolved_hostname,
        		last_registered)
        	values (?, ?, NOW())
        	on duplicate key update
        		unresolved_hostname=values(unresolved_hostname),
        		last_registered=now()
				`, instanceKey.Hostname, unresolvedHostname,
		)
		if err != nil {
			return log.Errore(err)
		}
		_, err = db.ExecOrchestrator(`
        	replace into hostname_unresolve_history (
        		hostname,
        		unresolved_hostname,
        		last_registered)
        	values (?, ?, NOW())
				`, instanceKey.Hostname, unresolvedHostname,
		)
		writeUnresolvedHostnameCounter.Inc(1)
		return nil
	}
	return ExecDBWriteFunc(writeFunc)
}

// DeleteHostnameUnresolve removes an unresolve entry
func DeleteHostnameUnresolve(instanceKey *InstanceKey) error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
      	delete from hostname_unresolve
				where hostname=?
				`, instanceKey.Hostname,
		)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// ExpireHostnameUnresolve expires hostname_unresolve entries that haven't been updated recently.
func ExpireHostnameUnresolve() error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
      	delete from hostname_unresolve
				where last_registered < NOW() - INTERVAL ? MINUTE
				`, config.Config.ExpiryHostnameResolvesMinutes,
		)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// ForgetExpiredHostnameResolves
func ForgetExpiredHostnameResolves() error {
	_, err := db.ExecOrchestrator(`
			delete
				from hostname_resolve
			where
				resolved_timestamp < NOW() - interval ? minute`,
		2*config.Config.ExpiryHostnameResolvesMinutes,
	)
	return err
}

// DeleteInvalidHostnameResolves removes invalid resolves. At this time these are:
// - infinite loop resolves (A->B and B->A), remove earlier mapping
func DeleteInvalidHostnameResolves() error {
	var invalidHostnames []string

	query := `
		select
		    early.hostname
		  from
		    hostname_resolve as latest
		    join hostname_resolve early on (latest.resolved_hostname = early.hostname and latest.hostname = early.resolved_hostname)
		  where
		    latest.hostname != latest.resolved_hostname
		    and latest.resolved_timestamp > early.resolved_timestamp
	   	`

	type invalidHostnameRow struct {
		Hostname string `gorm:"column:hostname"`
	}
	rows, err := db.QueryOrchestratorRows[invalidHostnameRow](context.Background(), query)
	for _, row := range rows {
		invalidHostnames = append(invalidHostnames, row.Hostname)
	}
	if err != nil {
		return err
	}

	for _, invalidHostname := range invalidHostnames {
		_, err = db.ExecOrchestrator(`
			delete
				from hostname_resolve
			where
				hostname = ?`,
			invalidHostname,
		)
		log.Errore(err)
	}
	return err
}

// deleteHostnameResolves compeltely erases the database cache
func deleteHostnameResolves() error {
	_, err := db.ExecOrchestrator(`
			delete
				from hostname_resolve`,
	)
	return err
}

// writeHostnameIPs stroes an ipv4 and ipv6 associated witha hostname, if available
func writeHostnameIPs(hostname string, ipv4String string, ipv6String string) error {
	writeFunc := func() error {
		_, err := db.ExecOrchestrator(`
			insert into
					hostname_ips (hostname, ipv4, ipv6, last_updated)
				values
					(?, ?, ?, NOW())
				on duplicate key update
					ipv4 = VALUES(ipv4),
					ipv6 = VALUES(ipv6),
					last_updated = VALUES(last_updated)
			`,
			hostname,
			ipv4String,
			ipv6String,
		)
		return log.Errore(err)
	}
	return ExecDBWriteFunc(writeFunc)
}

// readUnresolvedHostname reverse-reads hostname resolve. It returns a hostname which matches given pattern and resovles to resolvedHostname,
// or, in the event no such hostname is found, the given resolvedHostname, unchanged.
func readHostnameIPs(hostname string) (ipv4 string, ipv6 string, err error) {
	query := `
		select
			ipv4, ipv6
		from
			hostname_ips
		where
			hostname = ?
	`
	type hostnameIPRow struct {
		IPv4 string `gorm:"column:ipv4"`
		IPv6 string `gorm:"column:ipv6"`
	}
	rows, err := db.QueryOrchestratorRows[hostnameIPRow](context.Background(), query, hostname)
	if err == nil && len(rows) > 0 {
		ipv4 = rows[0].IPv4
		ipv6 = rows[0].IPv6
	}
	return ipv4, ipv6, log.Errore(err)
}
