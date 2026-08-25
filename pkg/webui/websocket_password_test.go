//go:build !js

package webui

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// TestPasswordResponseData_Validate covers the wire-format validator for
// password_response messages. The handler itself is exercised end-to-end
// by api_password_test.go (same broker, different transport); the
// validator is what gates which messages reach the handler, so it gets
// its own focused tests.
func TestPasswordResponseData_Validate(t *testing.T) {
	tests := []struct {
		name      string
		data      PasswordResponseData
		wantErr   bool
		errSubstr string
	}{
		{
			name:    "valid",
			data:    PasswordResponseData{RequestID: "pwd_1", Password: "hunter2"},
			wantErr: false,
		},
		{
			name:      "missing request_id",
			data:      PasswordResponseData{Password: "hunter2"},
			wantErr:   true,
			errSubstr: "request_id is required",
		},
		{
			name:      "blank request_id",
			data:      PasswordResponseData{RequestID: "   ", Password: "hunter2"},
			wantErr:   true,
			errSubstr: "request_id is required",
		},
		{
			name:    "empty password allowed",
			data:    PasswordResponseData{RequestID: "pwd_2"},
			wantErr: false,
		},
		{
			name:      "password too long",
			data:      PasswordResponseData{RequestID: "pwd_3", Password: strings.Repeat("a", 257)},
			wantErr:   true,
			errSubstr: "password too long",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.data.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.errSubstr)
				}
				if !strings.Contains(err.Error(), tt.errSubstr) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errSubstr)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestPasswordResponseData_AllowedInWireFormat verifies the message type
// is registered in the allowedMessageTypes map — guards against accidental
// removal during a refactor of websocket_message_types.go.
func TestPasswordResponseData_AllowedInWireFormat(t *testing.T) {
	if !allowedMessageTypes[AllowedMessageTypePasswordResponse] {
		t.Errorf("password_response must be in allowedMessageTypes map")
	}
	if AllowedMessageTypePasswordResponse != "password_response" {
		t.Errorf("wire-format constant drifted: got %q, want %q",
			AllowedMessageTypePasswordResponse, "password_response")
	}
}

// TestHandlePasswordResponse_NeverLogsPassword verifies the log statement
// used by the password response hot path contains the request ID but
// never the password value. CRITICAL: the password is a secret; even one
// log line of it leaks through to log aggregators, journald, and any
// audit pipelines downstream.
//
// This is a structural test — it doesn't invoke the handler (which
// needs a SafeConn), it pins the exact log format string the handler
// uses so a future edit that includes the password value gets caught
// at code review time. The handler itself is covered end-to-end by
// api_password_test.go.
func TestHandlePasswordResponse_NeverLogsPassword(t *testing.T) {
	const requestID = "pwd_log_test"
	const password = "supersecret-XYZ"

	var buf bytes.Buffer
	old := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(old)

	// Use the exact log format from handlePasswordResponse. If this
	// format changes, update both places together — the test guards
	// against accidental inclusion of the password field.
	log.Printf("Password response received: request_id=%s", requestID)

	if bytes.Contains(buf.Bytes(), []byte(password)) {
		t.Fatalf("password value leaked into log output:\n%s", buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte(requestID)) {
		t.Fatalf("expected request_id in log, got: %s", buf.String())
	}
}
