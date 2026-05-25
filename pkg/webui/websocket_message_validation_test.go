//go:build !js

package webui

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAndValidateMessage_ValidMessages(t *testing.T) {
	tests := []struct {
		name    string
		msg     []byte
		wantMsg string
	}{
		{
			name: "valid ping",
			msg:  []byte(`{"type":"ping"}`),
		},
		{
			name: "valid pong",
			msg:  []byte(`{"type":"pong"}`),
		},
		{
			name: "valid subscribe",
			msg:  []byte(`{"type":"subscribe","data":{"events":["event1","event2"]}}`),
		},
		{
			name: "valid provider_change",
			msg:  []byte(`{"type":"provider_change","data":{"provider":"openai"}}`),
		},
		{
			name: "valid model_change",
			msg:  []byte(`{"type":"model_change","data":{"model":"gpt-4"}}`),
		},
		{
			name: "valid model_change with provider",
			msg:  []byte(`{"type":"model_change","data":{"model":"gpt-4","provider":"openai"}}`),
		},
		{
			name: "valid persona_change",
			msg:  []byte(`{"type":"persona_change","data":{"persona":"coder"}}`),
		},
		{
			name: "valid security_approval_response",
			msg:  []byte(`{"type":"security_approval_response","data":{"request_id":"req123","approved":true}}`),
		},
		{
			name: "valid security_approval_response reject",
			msg:  []byte(`{"type":"security_approval_response","data":{"request_id":"req123","approved":false}}`),
		},
		{
			name: "valid security_prompt_response",
			msg:  []byte(`{"type":"security_prompt_response","data":{"request_id":"req123","response":true}}`),
		},
		{
			name: "valid ask_user_response",
			msg:  []byte(`{"type":"ask_user_response","data":{"request_id":"req123","response":"yes"}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := parseAndValidateMessage(tt.msg)
			if err != nil {
				t.Fatalf("parseAndValidateMessage() error = %v", err)
			}
			if msg == nil {
				t.Fatal("expected non-nil message")
			}
		})
	}
}

func TestParseAndValidateMessage_InvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "not valid JSON",
			msg:  []byte(`not json`),
		},
		{
			name: "malformed JSON",
			msg:  []byte(`{"type":"ping"`),
		},
		{
			name: "JSON with syntax error",
			msg:  []byte(`{"type":"ping",}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAndValidateMessage(tt.msg)
			if err == nil {
				t.Error("expected error for invalid JSON, got nil")
			}
		})
	}
}

func TestParseAndValidateMessage_MissingType(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "empty object",
			msg:  []byte(`{}`),
		},
		{
			name: "only data field",
			msg:  []byte(`{"data":{"events":["event1"]}}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAndValidateMessage(tt.msg)
			if err == nil {
				t.Error("expected error for missing type field, got nil")
			}
		})
	}
}

func TestParseAndValidateMessage_UnknownType(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "unknown type",
			msg:  []byte(`{"type":"unknown"}`),
		},
		{
			name: "invalid type",
			msg:  []byte(`{"type":"invalid_type"}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAndValidateMessage(tt.msg)
			if err == nil {
				t.Error("expected error for unknown type, got nil")
			}
		})
	}
}

func TestParseAndValidateMessage_EmptyType(t *testing.T) {
	tests := []struct {
		name string
		msg  []byte
	}{
		{
			name: "empty type string",
			msg:  []byte(`{"type":""}`),
		},
		{
			name: "whitespace only type",
			msg:  []byte(`{"type":"   "}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAndValidateMessage(tt.msg)
			if err == nil {
				t.Error("expected error for empty type, got nil")
			}
		})
	}
}

func TestParseAndValidateMessage_TypeTooLong(t *testing.T) {
	longType := string(make([]byte, maxMessageTypeLen+1))
	for i := range longType {
		longType = longType[:i] + "a" + longType[i+1:]
	}
	msg := []byte(`{"type":"` + longType + `"}`)
	_, err := parseAndValidateMessage(msg)
	if err == nil {
		t.Error("expected error for type too long, got nil")
	}
}

func TestParseAndValidateMessage_OversizedMessage(t *testing.T) {
	// Create a message larger than maxWebSocketMessageSize
	longData := strings.Repeat("a", maxWebSocketMessageSize+1)
	msg := []byte(`{"type":"ping","data":"` + longData + `"}`)
	_, err := parseAndValidateMessage(msg)
	if err == nil {
		t.Error("expected error for oversized message, got nil")
	}
}

func TestProviderChangeData_Validate(t *testing.T) {
	tests := []struct {
		name    string
		data    ProviderChangeData
		wantErr bool
	}{
		{
			name: "valid provider",
			data: ProviderChangeData{Provider: "openai"},
		},
		{
			name: "provider with whitespace trimmed",
			data: ProviderChangeData{Provider: "  openai  "},
		},
		{
			name:    "empty provider",
			data:    ProviderChangeData{Provider: ""},
			wantErr: true,
		},
		{
			name:    "whitespace only provider",
			data:    ProviderChangeData{Provider: "   "},
			wantErr: true,
		},
		{
			name: "provider exactly at limit",
			data: ProviderChangeData{Provider: string(make([]byte, maxProviderLen))},
		},
		{
			name:    "provider too long",
			data:    ProviderChangeData{Provider: string(make([]byte, maxProviderLen+1))},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "provider exactly at limit" || tt.name == "provider too long" {
				// Fill with valid characters
				for i := range tt.data.Provider {
					tt.data.Provider = tt.data.Provider[:i] + "a" + tt.data.Provider[i+1:]
				}
			}

			err := tt.data.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ProviderChangeData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestModelChangeData_Validate(t *testing.T) {
	tests := []struct {
		name    string
		data    ModelChangeData
		wantErr bool
	}{
		{
			name: "valid model",
			data: ModelChangeData{Model: "gpt-4"},
		},
		{
			name: "valid model with provider",
			data: ModelChangeData{Model: "gpt-4", Provider: "openai"},
		},
		{
			name: "provider with whitespace trimmed",
			data: ModelChangeData{Model: "gpt-4", Provider: "  openai  "},
		},
		{
			name:    "empty model",
			data:    ModelChangeData{Model: ""},
			wantErr: true,
		},
		{
			name:    "whitespace only model",
			data:    ModelChangeData{Model: "   "},
			wantErr: true,
		},
		{
			name: "model exactly at limit",
			data: ModelChangeData{Model: string(make([]byte, maxModelLen))},
		},
		{
			name:    "model too long",
			data:    ModelChangeData{Model: string(make([]byte, maxModelLen+1))},
			wantErr: true,
		},
		{
			name:    "provider too long",
			data:    ModelChangeData{Model: "gpt-4", Provider: string(make([]byte, maxProviderLen+1))},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "model exactly at limit" || tt.name == "model too long" || tt.name == "provider too long" {
				// Fill with valid characters
				for i := range tt.data.Model {
					tt.data.Model = tt.data.Model[:i] + "a" + tt.data.Model[i+1:]
				}
				if tt.data.Provider != "" {
					for i := range tt.data.Provider {
						tt.data.Provider = tt.data.Provider[:i] + "a" + tt.data.Provider[i+1:]
					}
				}
			}

			err := tt.data.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("ModelChangeData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPersonaChangeData_Validate(t *testing.T) {
	tests := []struct {
		name    string
		data    PersonaChangeData
		wantErr bool
	}{
		{
			name: "valid persona",
			data: PersonaChangeData{Persona: "coder"},
		},
		{
			name: "persona with whitespace trimmed",
			data: PersonaChangeData{Persona: "  coder  "},
		},
		{
			name:    "empty persona",
			data:    PersonaChangeData{Persona: ""},
			wantErr: true,
		},
		{
			name:    "whitespace only persona",
			data:    PersonaChangeData{Persona: "   "},
			wantErr: true,
		},
		{
			name: "persona exactly at limit",
			data: PersonaChangeData{Persona: string(make([]byte, maxPersonaLen))},
		},
		{
			name:    "persona too long",
			data:    PersonaChangeData{Persona: string(make([]byte, maxPersonaLen+1))},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "persona exactly at limit" || tt.name == "persona too long" {
				// Fill with valid characters
				for i := range tt.data.Persona {
					tt.data.Persona = tt.data.Persona[:i] + "a" + tt.data.Persona[i+1:]
				}
			}

			err := tt.data.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("PersonaChangeData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecurityApprovalResponseData_Validate(t *testing.T) {
	tests := []struct {
		name    string
		data    SecurityApprovalResponseData
		wantErr bool
	}{
		{
			name: "valid approval",
			data: SecurityApprovalResponseData{RequestID: "req123", Approved: true},
		},
		{
			name: "valid rejection",
			data: SecurityApprovalResponseData{RequestID: "req123", Approved: false},
		},
		{
			name: "request_id with whitespace trimmed",
			data: SecurityApprovalResponseData{RequestID: "  req123  ", Approved: true},
		},
		{
			name:    "empty request_id",
			data:    SecurityApprovalResponseData{RequestID: "", Approved: true},
			wantErr: true,
		},
		{
			name:    "whitespace only request_id",
			data:    SecurityApprovalResponseData{RequestID: "   ", Approved: true},
			wantErr: true,
		},
		{
			name: "request_id exactly at limit",
			data: SecurityApprovalResponseData{RequestID: string(make([]byte, maxRequestIDLen)), Approved: true},
		},
		{
			name:    "request_id too long",
			data:    SecurityApprovalResponseData{RequestID: string(make([]byte, maxRequestIDLen+1)), Approved: true},
			wantErr: true,
		},
		// SP-058 follow-up: 4-option Action field
		{
			name: "action approve_once",
			data: SecurityApprovalResponseData{RequestID: "req123", Action: "approve_once"},
		},
		{
			name: "action approve_always",
			data: SecurityApprovalResponseData{RequestID: "req123", Action: "approve_always"},
		},
		{
			name: "action elevate",
			data: SecurityApprovalResponseData{RequestID: "req123", Action: "elevate"},
		},
		{
			name: "action deny",
			data: SecurityApprovalResponseData{RequestID: "req123", Action: "deny"},
		},
		{
			name: "action empty (legacy bool path)",
			data: SecurityApprovalResponseData{RequestID: "req123", Approved: true},
		},
		{
			name:    "action invalid value",
			data:    SecurityApprovalResponseData{RequestID: "req123", Action: "yolo"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request_id exactly at limit" || tt.name == "request_id too long" {
				// Fill with valid characters
				for i := range tt.data.RequestID {
					tt.data.RequestID = tt.data.RequestID[:i] + "a" + tt.data.RequestID[i+1:]
				}
			}

			err := tt.data.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("SecurityApprovalResponseData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSecurityPromptResponseData_Validate(t *testing.T) {
	tests := []struct {
		name    string
		data    SecurityPromptResponseData
		wantErr bool
	}{
		{
			name: "valid response true",
			data: SecurityPromptResponseData{RequestID: "req123", Response: true},
		},
		{
			name: "valid response false",
			data: SecurityPromptResponseData{RequestID: "req123", Response: false},
		},
		{
			name: "request_id with whitespace trimmed",
			data: SecurityPromptResponseData{RequestID: "  req123  ", Response: true},
		},
		{
			name:    "empty request_id",
			data:    SecurityPromptResponseData{RequestID: "", Response: true},
			wantErr: true,
		},
		{
			name:    "whitespace only request_id",
			data:    SecurityPromptResponseData{RequestID: "   ", Response: true},
			wantErr: true,
		},
		{
			name: "request_id exactly at limit",
			data: SecurityPromptResponseData{RequestID: string(make([]byte, maxRequestIDLen)), Response: true},
		},
		{
			name:    "request_id too long",
			data:    SecurityPromptResponseData{RequestID: string(make([]byte, maxRequestIDLen+1)), Response: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request_id exactly at limit" || tt.name == "request_id too long" {
				// Fill with valid characters
				for i := range tt.data.RequestID {
					tt.data.RequestID = tt.data.RequestID[:i] + "a" + tt.data.RequestID[i+1:]
				}
			}

			err := tt.data.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("SecurityPromptResponseData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAskUserResponseData_Validate(t *testing.T) {
	tests := []struct {
		name    string
		data    AskUserResponseData
		wantErr bool
	}{
		{
			name: "valid response",
			data: AskUserResponseData{RequestID: "req123", Response: "yes"},
		},
		{
			name: "response with whitespace trimmed",
			data: AskUserResponseData{RequestID: "  req123  ", Response: "  yes  "},
		},
		{
			name:    "empty request_id",
			data:    AskUserResponseData{RequestID: "", Response: "yes"},
			wantErr: true,
		},
		{
			name:    "whitespace only request_id",
			data:    AskUserResponseData{RequestID: "   ", Response: "yes"},
			wantErr: true,
		},
		{
			name:    "empty response",
			data:    AskUserResponseData{RequestID: "req123", Response: ""},
			wantErr: true,
		},
		{
			name:    "whitespace only response",
			data:    AskUserResponseData{RequestID: "req123", Response: "   "},
			wantErr: true,
		},
		{
			name: "request_id exactly at limit",
			data: AskUserResponseData{RequestID: string(make([]byte, maxRequestIDLen)), Response: "yes"},
		},
		{
			name:    "request_id too long",
			data:    AskUserResponseData{RequestID: string(make([]byte, maxRequestIDLen+1)), Response: "yes"},
			wantErr: true,
		},
		{
			name: "response exactly at limit",
			data: AskUserResponseData{RequestID: "req123", Response: string(make([]byte, maxResponseLen))},
		},
		{
			name:    "response too long",
			data:    AskUserResponseData{RequestID: "req123", Response: string(make([]byte, maxResponseLen+1))},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "request_id exactly at limit" || tt.name == "request_id too long" {
				// Fill with valid characters
				for i := range tt.data.RequestID {
					tt.data.RequestID = tt.data.RequestID[:i] + "a" + tt.data.RequestID[i+1:]
				}
			}
			if tt.name == "response exactly at limit" || tt.name == "response too long" {
				// Fill with valid characters
				for i := range tt.data.Response {
					tt.data.Response = tt.data.Response[:i] + "a" + tt.data.Response[i+1:]
				}
			}

			err := tt.data.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("AskUserResponseData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSubscribeData_Validate(t *testing.T) {
	tests := []struct {
		name    string
		data    SubscribeData
		wantErr bool
	}{
		{
			name: "valid events",
			data: SubscribeData{Events: []string{"event1", "event2"}},
		},
		{
			name: "empty events array",
			data: SubscribeData{Events: []string{}},
		},
		{
			name: "events with whitespace trimmed",
			data: SubscribeData{Events: []string{"  event1  ", "  event2  "}},
		},
		{
			name: "exactly at max events",
			data: SubscribeData{Events: make([]string, maxEventsCount)},
		},
		{
			name:    "too many events",
			data:    SubscribeData{Events: make([]string, maxEventsCount+1)},
			wantErr: true,
		},
		{
			name:    "empty event in array",
			data:    SubscribeData{Events: []string{"event1", "", "event2"}},
			wantErr: true,
		},
		{
			name:    "whitespace only event",
			data:    SubscribeData{Events: []string{"event1", "   ", "event2"}},
			wantErr: true,
		},
		{
			name:    "event too long",
			data:    SubscribeData{Events: []string{"event1", string(make([]byte, maxEventNameLen+1))}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "exactly at max events" {
				// Fill with valid events
				for i := range tt.data.Events {
					tt.data.Events[i] = "event"
				}
			}
			if tt.name == "event too long" {
				// Fill with valid characters
				for i := range tt.data.Events[1] {
					tt.data.Events[1] = tt.data.Events[1][:i] + "a" + tt.data.Events[1][i+1:]
				}
			}

			err := tt.data.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("SubscribeData.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseAndValidateData_MissingDataPayload(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
	}{
		{
			name: "nil data",
			data: nil,
		},
		{
			name: "empty data",
			data: json.RawMessage([]byte(`null`)),
		},
		{
			name: "empty bytes",
			data: json.RawMessage([]byte(``)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAndValidateData[ProviderChangeData](tt.data, func(d *ProviderChangeData) error {
				return d.Validate()
			})
			if err == nil {
				t.Error("expected error for missing data payload, got nil")
			}
		})
	}
}

func TestParseAndValidateData_InvalidJSON(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
	}{
		{
			name: "malformed JSON",
			data: json.RawMessage([]byte(`{invalid`)),
		},
		{
			name: "wrong type for field",
			data: json.RawMessage([]byte(`{"provider":123}`)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseAndValidateData[ProviderChangeData](tt.data, func(d *ProviderChangeData) error {
				return d.Validate()
			})
			if err == nil {
				t.Error("expected error for invalid data JSON, got nil")
			}
		})
	}
}
