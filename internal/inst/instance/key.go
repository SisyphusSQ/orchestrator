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

package instance

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// InstanceKey is an instance indicator, identifued by hostname and port
type InstanceKey struct {
	Hostname string
	Port     int
}

var (
	ipv4Regexp         = regexp.MustCompile("^([0-9]+)[.]([0-9]+)[.]([0-9]+)[.]([0-9]+)$")
	ipv4HostPortRegexp = regexp.MustCompile("^([^:]+):([0-9]+)$")
	ipv4HostRegexp     = regexp.MustCompile("^([^:]+)$")
	ipv6HostPortRegexp = regexp.MustCompile(`^\[([:0-9a-fA-F]+)\]:([0-9]+)$`) // e.g. [2001:db8:1f70::999:de8:7648:6e8]:3308
	ipv6HostRegexp     = regexp.MustCompile("^([:0-9a-fA-F]+)$")              // e.g. 2001:db8:1f70::999:de8:7648:6e8
)

const detachHint = "//"

func NewInstanceKey(hostname string, port int) (*InstanceKey, error) {
	if hostname == "" {
		return nil, fmt.Errorf("empty hostname")
	}
	return &InstanceKey{Hostname: hostname, Port: port}, nil
}

func NewInstanceKeyStrings(hostname string, port string) (*InstanceKey, error) {
	if portInt, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("invalid port: %s", port)
	} else {
		return NewInstanceKey(hostname, portInt)
	}
}

func ParseInstanceKey(hostPort string, defaultPort int) (*InstanceKey, error) {
	hostname := ""
	port := ""
	if submatch := ipv4HostPortRegexp.FindStringSubmatch(hostPort); len(submatch) > 0 {
		hostname = submatch[1]
		port = submatch[2]
	} else if submatch := ipv4HostRegexp.FindStringSubmatch(hostPort); len(submatch) > 0 {
		hostname = submatch[1]
	} else if submatch := ipv6HostPortRegexp.FindStringSubmatch(hostPort); len(submatch) > 0 {
		hostname = submatch[1]
		port = submatch[2]
	} else if submatch := ipv6HostRegexp.FindStringSubmatch(hostPort); len(submatch) > 0 {
		hostname = submatch[1]
	} else {
		return nil, fmt.Errorf("cannot parse address: %s", hostPort)
	}
	if port == "" {
		port = strconv.Itoa(defaultPort)
	}
	return NewInstanceKeyStrings(hostname, port)
}

// Equals tests equality between this key and another key
func (key *InstanceKey) Equals(other *InstanceKey) bool {
	if other == nil {
		return false
	}
	return key.Hostname == other.Hostname && key.Port == other.Port
}

// SmallerThan returns true if this key is dictionary-smaller than another.
// This is used for consistent sorting/ordering; there's nothing magical about it.
func (key *InstanceKey) SmallerThan(other *InstanceKey) bool {
	if key.Hostname < other.Hostname {
		return true
	}
	if key.Hostname == other.Hostname && key.Port < other.Port {
		return true
	}
	return false
}

// IsDetached returns 'true' when this hostname is logically "detached"
func (key *InstanceKey) IsDetached() bool {
	return strings.HasPrefix(key.Hostname, detachHint)
}

// IsValid uses simple heuristics to see whether this key represents an actual instance
func (key *InstanceKey) IsValid() bool {
	if key.Hostname == "_" {
		return false
	}
	if key.IsDetached() {
		return false
	}
	return len(key.Hostname) > 0 && key.Port > 0
}

// DetachedKey returns an instance key whose hostname is detached: invalid, but recoverable
func (key *InstanceKey) DetachedKey() *InstanceKey {
	if key.IsDetached() {
		return key
	}
	return &InstanceKey{Hostname: fmt.Sprintf("%s%s", detachHint, key.Hostname), Port: key.Port}
}

// ReattachedKey returns an instance key whose hostname is detached: invalid, but recoverable
func (key *InstanceKey) ReattachedKey() *InstanceKey {
	if !key.IsDetached() {
		return key
	}
	return &InstanceKey{Hostname: key.Hostname[len(detachHint):], Port: key.Port}
}

// StringCode returns an official string representation of this key
func (key *InstanceKey) StringCode() string {
	return fmt.Sprintf("%s:%d", key.Hostname, key.Port)
}

// DisplayString returns a user-friendly string representation of this key
func (key *InstanceKey) DisplayString() string {
	return key.StringCode()
}

// String returns a user-friendly string representation of this key
func (key InstanceKey) String() string {
	return key.StringCode()
}

// IsValid uses simple heuristics to see whether this key represents an actual instance
func (key *InstanceKey) IsIPv4() bool {
	return ipv4Regexp.MatchString(key.Hostname)
}
