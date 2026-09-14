package http

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/openark/orchestrator/internal/http/contract"
	"github.com/openark/orchestrator/internal/http/transport"
	httpweb "github.com/openark/orchestrator/internal/http/web"
	instaudit "github.com/openark/orchestrator/internal/inst/audit"
	instmaintenance "github.com/openark/orchestrator/internal/inst/maintenance"
	"github.com/openark/orchestrator/internal/models/domain"
	"github.com/openark/orchestrator/internal/models/vo"
	orcraft "github.com/openark/orchestrator/internal/raft"
	"github.com/openark/orchestrator/internal/recoverypolicy"
)

func TestAPIContractRoutes(t *testing.T) {
	spec, err := contract.ReadSpecification()
	if err != nil {
		t.Fatal(err)
	}
	router, err := transport.NewRouter(transport.RouterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	api := Routes{}
	api.RegisterRequests(router)
	httpweb.New("", nil).RegisterRequests(router)
	actual := map[string]bool{}
	for _, route := range router.LogicalRoutes() {
		if strings.HasPrefix(route.Path, "/api/") {
			actual[route.Method+" "+route.Path] = true
		}
	}
	for _, endpoint := range spec.Endpoints {
		key := endpoint.Method + " " + endpoint.Path
		if !actual[key] {
			t.Errorf("contract route not registered: %s", key)
		}
		delete(actual, key)
		if endpoint.Method != "GET" && endpoint.ReadOnly {
			t.Errorf("write endpoint declared read-only: %s", key)
		}
	}
	for key := range actual {
		t.Errorf("registered route missing from contract: %s", key)
	}
	if !reflect.DeepEqual(spec.Aliases, apiSynonyms) {
		t.Error("route synonyms differ from API contract")
	}
}

// Validate the serialized server output, not Go source spelling. Extra server
// fields are allowed because the Web schemas describe the fields it consumes.
func TestAPIContractResponseShapes(t *testing.T) {
	spec, err := contract.ReadSpecification()
	if err != nil {
		t.Fatal(err)
	}
	samples := map[string]any{
		"NullableNumber": sql.NullInt64{}, "Maintenance": instmaintenance.Maintenance{}, "Audit": instaudit.Audit{},
		"InstanceKey": vo.InstanceKey{}, "Coordinates": vo.BinlogCoordinates{},
		"Instance": vo.Instance{}, "Cluster": vo.ClusterInfo{},
		"Analysis": vo.ReplicationAnalysis{}, "Recovery": vo.TopologyRecovery{},
		"RecoveryStep": vo.TopologyRecoveryStep{}, "BlockedRecovery": vo.BlockedTopologyRecovery{},
		"Seed": vo.SeedOperation{}, "SeedState": vo.SeedOperationState{}, "Agent": vo.Agent{},
		"WebConfig": httpweb.Config{}, "RecoveryPolicy": recoverypolicy.Defaults(),
		"RecoveryPolicyDocument": vo.RecoveryPolicyDocument{ScopeType: "global", Effective: recoverypolicy.Defaults(), Inherited: recoverypolicy.Defaults()},
		"HookProfile":            domain.RecoveryHookProfile{FailurePolicy: "abort"},
		"HookAssignment":         domain.RecoveryHookAssignment{ScopeType: "global", Mode: "inherit"},
		"Envelope":               &contract.Response{Code: contract.OK, Message: "ok"},
	}
	for name := range spec.Definitions {
		if _, ok := samples[name]; !ok {
			t.Errorf("response schema lacks a server sample: %s", name)
		}
	}
	for _, class := range []orcraft.Class{orcraft.ClassInvalidArgument, orcraft.ClassUnavailable, orcraft.ClassNotBootstrapped, orcraft.ClassNotLeader, orcraft.ClassConflict, orcraft.ClassNotFound, orcraft.ClassFailed, orcraft.ClassIndeterminate} {
		value := map[string]any{"Code": "ERROR", "Message": "failed", "Details": nil, "ErrorClass": string(class)}
		if err := validateSchema(spec.Definitions["Envelope"], value, spec.Definitions); err != nil {
			t.Fatal(err)
		}
	}
	for name, sample := range samples {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(sample)
			if err != nil {
				t.Fatal(err)
			}
			var value any
			if err := json.Unmarshal(data, &value); err != nil {
				t.Fatal(err)
			}
			if err := validateSchema(spec.Definitions[name], value, spec.Definitions); err != nil {
				t.Fatal(err)
			}
		})
	}
	t.Run("detect drift", func(t *testing.T) {
		for _, value := range []any{map[string]any{"Hostname": "db"}, map[string]any{"Hostname": "db", "Port": "3306"}} {
			if validateSchema(spec.Definitions["InstanceKey"], value, spec.Definitions) == nil {
				t.Fatal("invalid response accepted")
			}
		}
	})
}

func validateSchema(schema map[string]any, value any, defs map[string]map[string]any) error {
	if ref, ok := schema["$ref"].(string); ok {
		return validateSchema(defs[strings.TrimPrefix(ref, "#/$defs/")], value, defs)
	}
	if name, ok := schema["x-partial"].(string); ok {
		partial := map[string]any{}
		for k, v := range defs[name] {
			if k != "required" {
				partial[k] = v
			}
		}
		return validateSchema(partial, value, defs)
	}
	if variants, ok := schema["anyOf"].([]any); ok {
		for _, variant := range variants {
			if validateSchema(variant.(map[string]any), value, defs) == nil {
				return nil
			}
		}
		return fmt.Errorf("no variant matches %v", value)
	}
	if types, ok := schema["type"].([]any); ok {
		for _, kind := range types {
			candidate := map[string]any{}
			for k, v := range schema {
				candidate[k] = v
			}
			candidate["type"] = kind
			if validateSchema(candidate, value, defs) == nil {
				return nil
			}
		}
		return fmt.Errorf("value %T does not match %v", value, types)
	}
	if enums, ok := schema["enum"].([]any); ok {
		matched := false
		for _, option := range enums {
			matched = matched || reflect.DeepEqual(option, value)
		}
		if !matched {
			return fmt.Errorf("value %v not in %v", value, enums)
		}
	}
	switch schema["type"] {
	case "null":
		if value != nil {
			return fmt.Errorf("expected null, got %T", value)
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}
	case "number", "integer":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("expected number, got %T", value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("expected boolean, got %T", value)
		}
	case "array":
		values, ok := value.([]any)
		if !ok {
			return fmt.Errorf("expected array, got %T", value)
		}
		for _, item := range values {
			if err := validateSchema(schema["items"].(map[string]any), item, defs); err != nil {
				return err
			}
		}
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("expected object, got %T", value)
		}
		if required, ok := schema["required"].([]any); ok {
			for _, field := range required {
				if _, exists := object[field.(string)]; !exists {
					return fmt.Errorf("missing field %s", field)
				}
			}
		}
		if properties, ok := schema["properties"].(map[string]any); ok {
			for key, child := range properties {
				if actual, exists := object[key]; exists {
					if err := validateSchema(child.(map[string]any), actual, defs); err != nil {
						return fmt.Errorf("%s: %w", key, err)
					}
				}
			}
		}
	}
	return nil
}
