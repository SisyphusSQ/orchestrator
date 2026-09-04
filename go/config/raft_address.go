package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// NormalizeRaftAddress ensures a raft bind/advertise value is a host:port pair.
// If the value has no port and defaultPort is set, that port is appended.
// The result is never used as a raft node identity.
func NormalizeRaftAddress(addr string, defaultPort int) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("raft address is empty")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		if defaultPort <= 0 {
			return "", fmt.Errorf("raft address %q must be host:port", addr)
		}
		var ok bool
		host, ok = raftHostWithoutPort(addr)
		if !ok {
			return "", fmt.Errorf("raft address %q is malformed", addr)
		}
		port = strconv.Itoa(defaultPort)
	}
	if host == "" {
		return "", fmt.Errorf("raft address %q is missing host", addr)
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return "", fmt.Errorf("raft address %q has invalid host", addr)
	}
	if port == "" {
		return "", fmt.Errorf("raft address %q is missing port", addr)
	}
	portNumber, convErr := strconv.ParseUint(port, 10, 16)
	if convErr != nil || portNumber == 0 {
		return "", fmt.Errorf("raft address %q has invalid port", addr)
	}
	return net.JoinHostPort(host, strconv.FormatUint(portNumber, 10)), nil
}

func raftHostWithoutPort(addr string) (string, bool) {
	host := addr
	if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
		if !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
			return "", false
		}
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
		if !isIPLiteral(host) {
			return "", false
		}
		return host, true
	}
	if isIPLiteral(host) {
		return host, true
	}
	if strings.Contains(host, ":") {
		return "", false
	}
	return host, true
}

func isIPLiteral(host string) bool {
	if zoneIndex := strings.LastIndex(host, "%"); zoneIndex >= 0 {
		if zoneIndex == len(host)-1 {
			return false
		}
		host = host[:zoneIndex]
	}
	return net.ParseIP(host) != nil
}
