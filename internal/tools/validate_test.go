package tools

import (
	"encoding/json"
	"testing"
)

func TestValidateParams(t *testing.T) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string"},
			"count": {"type": "integer"},
			"verbose": {"type": "boolean"},
			"mode": {"type": "string", "enum": ["read", "write"]}
		},
		"required": ["path"]
	}`)

	tests := []struct {
		name    string
		params  string
		wantErr bool
	}{
		{"valid", `{"path": "/tmp/test"}`, false},
		{"valid with all", `{"path": "/tmp/test", "count": 5, "verbose": true}`, false},
		{"missing required", `{"count": 5}`, true},
		{"wrong type string", `{"path": 123}`, true},
		{"wrong type int", `{"path": "/tmp", "count": "five"}`, true},
		{"wrong type bool", `{"path": "/tmp", "verbose": "yes"}`, true},
		{"valid enum", `{"path": "/tmp", "mode": "read"}`, false},
		{"invalid enum", `{"path": "/tmp", "mode": "execute"}`, true},
		{"not an object", `"hello"`, true},
		{"empty object", `{}`, true},
		{"null required", `{"path": null}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateParams(schema, json.RawMessage(tt.params))
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateParams() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateParams_EmptySchema(t *testing.T) {
	// No schema → no validation
	err := ValidateParams(nil, json.RawMessage(`{"anything": true}`))
	if err != nil {
		t.Errorf("expected no error for nil schema, got %v", err)
	}
}
