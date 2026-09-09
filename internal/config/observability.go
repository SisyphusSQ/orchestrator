package config

import (
	"fmt"
	"math"
	"net/url"
)

func (c *Configuration) validateTelemetry() error {
	if math.IsNaN(c.Observability.Tracing.SampleRatio) || c.Observability.Tracing.SampleRatio < 0 || c.Observability.Tracing.SampleRatio > 1 {
		return fmt.Errorf("observability.tracing.sampleRatio must be between 0 and 1")
	}
	if c.Observability.Tracing.Endpoint == "" {
		return nil
	}
	u, err := url.Parse(c.Observability.Tracing.Endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/traces" {
		return fmt.Errorf("observability.tracing.endpoint must be an http(s) URL ending in /v1/traces without userinfo, query or fragment")
	}
	return nil
}

var telemetryConfigurationLocked bool
