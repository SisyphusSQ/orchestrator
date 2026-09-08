package config

import (
	"fmt"
	"math"
	"net/url"
)

func (c *Configuration) validateTelemetry() error {
	if math.IsNaN(c.OTelTraceSampleRatio) || c.OTelTraceSampleRatio < 0 || c.OTelTraceSampleRatio > 1 {
		return fmt.Errorf("OTelTraceSampleRatio must be between 0 and 1")
	}
	if c.OTelTraceEndpoint == "" {
		return nil
	}
	u, err := url.Parse(c.OTelTraceEndpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/traces" {
		return fmt.Errorf("OTelTraceEndpoint must be an http(s) URL ending in /v1/traces without userinfo, query or fragment")
	}
	return nil
}

var telemetryConfigurationLocked bool
