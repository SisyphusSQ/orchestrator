// Package domain contains business-facing models independent of persistence
// and transport details.
package domain

// HostAttributes describes an attribute submitted by a host.
type HostAttributes struct {
	Hostname        string
	AttributeName   string
	AttributeValue  string
	SubmitTimestamp string
	ExpireTimestamp string
}
