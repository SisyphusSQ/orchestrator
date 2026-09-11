package presenter

import (
	"fmt"
	"os"

	fqdn "github.com/Showmax/go-fqdn"
	"github.com/openark/orchestrator/internal/config"
	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/transport"
)

var messagePrefix string

// Respond writes an API response using the status implied by its response code.
func Respond(r transport.Responder, response *contract.Response) {
	RespondStatus(r, response.Code.HTTPStatus(), response)
}

// RespondStatus writes an API response using an explicit HTTP status.
func RespondStatus(r transport.Responder, status int, response *contract.Response) {
	response.Message = fmt.Sprintf("%+v%+v", messagePrefix, response.Message)
	WriteJSON(r, status, response)
}

// SetupMessagePrefix resolves the configured orchestrator identity once route
// registration has completed.
func SetupMessagePrefix() {
	mode := config.Config.Server.ResponseIdentity.Mode
	if mode == "" || mode == "none" {
		return
	}
	if mode != "FQDN" && mode != "hostname" && mode != "custom" {
		log.Warning("PrependMessagesWithOrcIdentity option has unsupported value '%+v'")
		return
	}

	var hostname string
	var err error
	fallbackActive := false

	if mode == "FQDN" {
		if hostname, err = fqdn.FqdnHostname(); err != nil {
			log.Warning("Failed to get Orchestrator's FQDN. Falling back to hostname.")
			hostname = ""
			fallbackActive = true
		}
	}
	if fallbackActive || mode == "hostname" {
		fallbackActive = false
		if hostname, err = os.Hostname(); err != nil {
			log.Warning("Failed to get Orchestrator's FQDN. Falling back to custom prefix (if provided).")
			hostname = ""
			fallbackActive = true
		}
	}
	if (fallbackActive || mode == "custom") && config.Config.Server.ResponseIdentity.Custom != "" {
		hostname = config.Config.Server.ResponseIdentity.Custom
	}
	if hostname != "" {
		messagePrefix = fmt.Sprintf("Orchestrator %+v says: ", hostname)
	} else {
		log.Warning("Prepending messages with Orchestrator identity was requested, but identity cannot be determined. Skipping prefix.")
	}
}
