package contract

import (
	"embed"
	"encoding/json"
)

// Source is shared by the server checks and the client generator.
//
//go:embed spec.json
var Source embed.FS

type Endpoint struct {
	Method   string `json:"method"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
	Response struct {
		Kind   string         `json:"kind"`
		Schema map[string]any `json:"schema"`
	} `json:"response"`
}

type Specification struct {
	Definitions map[string]map[string]any `json:"$defs"`
	WebActions  []string                  `json:"webActions"`
	Aliases     map[string]string         `json:"aliases"`
	Endpoints   []Endpoint                `json:"endpoints"`
}

func ReadSpecification() (Specification, error) {
	data, err := Source.ReadFile("spec.json")
	if err != nil {
		return Specification{}, err
	}
	var spec Specification
	err = json.Unmarshal(data, &spec)
	return spec, err
}

// WebActions returns a fresh map; the embedded contract never changes at runtime.
func WebActions() map[string]bool {
	spec, err := ReadSpecification()
	if err != nil {
		panic(err)
	}
	result := make(map[string]bool, len(spec.WebActions))
	for _, name := range spec.WebActions {
		result[name] = true
	}
	return result
}
