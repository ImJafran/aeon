package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateParams performs lightweight JSON schema validation: checks required fields,
// basic types (string/integer/number/boolean/object/array), and enum values.
func ValidateParams(schema, params json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	if len(params) == 0 {
		params = json.RawMessage(`{}`)
	}

	var s schemaObj
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil // can't parse schema, skip validation
	}

	// Only validate object schemas
	if s.Type != "object" && s.Type != "" {
		return nil
	}

	var p map[string]json.RawMessage
	if err := json.Unmarshal(params, &p); err != nil {
		return fmt.Errorf("parameters must be a JSON object")
	}

	// Check required fields
	for _, req := range s.Required {
		val, exists := p[req]
		if !exists || isNull(val) {
			return fmt.Errorf("missing required parameter: %q", req)
		}
	}

	// Check types of provided fields
	for name, propSchema := range s.Properties {
		val, exists := p[name]
		if !exists || isNull(val) {
			continue
		}
		if err := validateType(name, propSchema, val); err != nil {
			return err
		}
	}

	return nil
}

type schemaObj struct {
	Type       string                     `json:"type"`
	Properties map[string]json.RawMessage `json:"properties"`
	Required   []string                   `json:"required"`
}

type propSchema struct {
	Type string   `json:"type"`
	Enum []string `json:"enum"`
}

func validateType(name string, rawSchema json.RawMessage, value json.RawMessage) error {
	var ps propSchema
	if err := json.Unmarshal(rawSchema, &ps); err != nil {
		return nil // can't parse property schema, skip
	}

	if ps.Type == "" {
		return nil
	}

	v := strings.TrimSpace(string(value))

	switch ps.Type {
	case "string":
		if v == "" || v[0] != '"' {
			return fmt.Errorf("parameter %q must be a string", name)
		}
		// Check enum
		if len(ps.Enum) > 0 {
			var s string
			json.Unmarshal(value, &s)
			found := false
			for _, e := range ps.Enum {
				if s == e {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("parameter %q must be one of: %s", name, strings.Join(ps.Enum, ", "))
			}
		}
	case "integer", "number":
		if v == "" || v[0] == '"' || v[0] == '{' || v[0] == '[' || v == "true" || v == "false" || v == "null" {
			return fmt.Errorf("parameter %q must be a %s", name, ps.Type)
		}
	case "boolean":
		if v != "true" && v != "false" {
			return fmt.Errorf("parameter %q must be a boolean", name)
		}
	case "object":
		if v == "" || v[0] != '{' {
			return fmt.Errorf("parameter %q must be an object", name)
		}
	case "array":
		if v == "" || v[0] != '[' {
			return fmt.Errorf("parameter %q must be an array", name)
		}
	}

	return nil
}

func isNull(v json.RawMessage) bool {
	return strings.TrimSpace(string(v)) == "null"
}
