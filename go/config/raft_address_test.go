package config

import "testing"

func TestNormalizeRaftAddress(t *testing.T) {
	tests := []struct {
		name        string
		addr        string
		defaultPort int
		want        string
		wantErr     bool
	}{
		{name: "host and port", addr: "127.0.0.1:10008", defaultPort: 10008, want: "127.0.0.1:10008"},
		{name: "host with default port", addr: "127.0.0.1", defaultPort: 10008, want: "127.0.0.1:10008"},
		{name: "hostname with port", addr: "orc-1.example:10009", defaultPort: 10008, want: "orc-1.example:10009"},
		{name: "ipv6", addr: "[::1]:10008", defaultPort: 10008, want: "[::1]:10008"},
		{name: "bare ipv6 with default port", addr: "::1", defaultPort: 10008, want: "[::1]:10008"},
		{name: "bracketed ipv6 with default port", addr: "[::1]", defaultPort: 10008, want: "[::1]:10008"},
		{name: "empty", addr: "", defaultPort: 10008, wantErr: true},
		{name: "missing port without default", addr: "127.0.0.1", defaultPort: 0, wantErr: true},
		{name: "invalid port", addr: "127.0.0.1:abc", defaultPort: 10008, wantErr: true},
		{name: "zero port", addr: "127.0.0.1:0", defaultPort: 10008, wantErr: true},
		{name: "port out of range", addr: "127.0.0.1:65536", defaultPort: 10008, wantErr: true},
		{name: "default port out of range", addr: "127.0.0.1", defaultPort: 65536, wantErr: true},
		{name: "malformed multiple ports", addr: "127.0.0.1:10008:10009", defaultPort: 10008, wantErr: true},
		{name: "host whitespace", addr: "node one:10008", defaultPort: 10008, wantErr: true},
		{name: "missing host", addr: ":10008", defaultPort: 10008, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeRaftAddress(tc.addr, tc.defaultPort)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("NormalizeRaftAddress(%q) error = nil, want error", tc.addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeRaftAddress(%q) error = %v", tc.addr, err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeRaftAddress(%q) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}
