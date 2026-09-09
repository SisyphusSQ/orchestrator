package config

import (
	"strings"
	"testing"
)

func TestRemovedTelemetryConfiguration(t *testing.T) {
	for _, field := range []string{"GraphiteAddr", "graphitepollseconds", "GRAPHITEPATH", "GraphiteConvertHostnameDotsToUnderscores", "DiscoveryCollectionRetentionSeconds", "DiscoveryQueueMaxStatisticsSize"} {
		t.Run(field, func(t *testing.T) {
			if err := decodeConfiguration(strings.NewReader(`{"`+field+`":null}`), newConfiguration()); err == nil {
				t.Fatal("removed field accepted")
			}
		})
	}
}
func TestTelemetryConfigValidation(t *testing.T) {
	for _, endpoint := range []string{"http://localhost:4318/v1/traces", "https://collector.example/v1/traces", ""} {
		c := newConfiguration()
		c.Observability.Tracing.Endpoint = endpoint
		if err := c.validateTelemetry(); err != nil {
			t.Fatal(err)
		}
	}
	for _, endpoint := range []string{"localhost:4318", "http://user:secret@host/v1/traces", "https://host/v1/traces?token=secret", "https://host/wrong", "ftp://host/v1/traces"} {
		c := newConfiguration()
		c.Observability.Tracing.Endpoint = endpoint
		if err := c.validateTelemetry(); err == nil {
			t.Fatalf("accepted invalid endpoint %s", endpoint)
		}
	}
}
