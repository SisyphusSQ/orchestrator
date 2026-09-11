package request

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/openark/orchestrator/internal/http/transport"
	instmodel "github.com/openark/orchestrator/internal/inst/instance"
	instinventory "github.com/openark/orchestrator/internal/inst/inventory"
	instresolve "github.com/openark/orchestrator/internal/inst/resolve"
	insttag "github.com/openark/orchestrator/internal/inst/tag"
)

var emptyInstanceKey instmodel.InstanceKey

// ResolveInstanceKey resolves and normalizes an instance route key.
func ResolveInstanceKey(host, port string) (instmodel.InstanceKey, error) {
	return resolveInstanceKey(host, port, true)
}

// ResolveRawInstanceKey parses and normalizes an instance route key without
// hostname resolution.
func ResolveRawInstanceKey(host, port string) (instmodel.InstanceKey, error) {
	return resolveInstanceKey(host, port, false)
}

func resolveInstanceKey(host, port string, resolve bool) (instmodel.InstanceKey, error) {
	var instanceKey *instmodel.InstanceKey
	var err error
	if resolve {
		instanceKey, err = instresolve.NewInstanceKeyStrings(host, port)
	} else {
		instanceKey, err = instresolve.NewRawInstanceKeyStrings(host, port)
	}
	if err != nil {
		return emptyInstanceKey, err
	}
	instanceKey, err = instinventory.FigureInstanceKey(instanceKey, nil)
	if err != nil {
		return emptyInstanceKey, err
	}
	if instanceKey == nil {
		return emptyInstanceKey, fmt.Errorf("unexpected nil instanceKey in getInstanceKeyInternal(%+v, %+v, %+v)", host, port, resolve)
	}
	return *instanceKey, nil
}

// Tag parses either the query-string form or the named route parameters.
func Tag(params transport.Params, req *http.Request) (*insttag.Tag, error) {
	if value := req.URL.Query().Get("tag"); value != "" {
		return insttag.ParseTag(value)
	}
	return insttag.NewTag(params["tagName"], params["tagValue"])
}

// BinlogCoordinates parses route parameters into binlog coordinates.
func BinlogCoordinates(logFile, logPos string) (instmodel.BinlogCoordinates, error) {
	coordinates := instmodel.BinlogCoordinates{LogFile: logFile}
	position, err := strconv.ParseInt(logPos, 10, 0)
	if err != nil {
		return coordinates, fmt.Errorf("invalid logPos: %s", logPos)
	}
	coordinates.LogPos = position
	return coordinates, nil
}
