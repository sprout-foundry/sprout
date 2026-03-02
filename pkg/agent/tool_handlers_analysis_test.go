package agent

import "testing"

func TestNormalizeVisionToolOutput_FailureBecomesError(t *testing.T) {
	raw := `{"success":false,"error_code":"VISION_REQUEST_FAILED","error_message":"vision model returned empty response"}`

	_, err := normalizeVisionToolOutput(raw, true)
	if err == nil {
		t.Fatal("expected error for unsuccessful vision response")
	}
}

func TestNormalizeVisionToolOutput_SuccessPlainTextPreferred(t *testing.T) {
	raw := `{"success":true,"extracted_text":"Login form with email and password fields"}`

	out, err := normalizeVisionToolOutput(raw, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "Login form with email and password fields" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestNormalizeVisionToolOutput_SuccessRawWhenStructuredPreferred(t *testing.T) {
	raw := `{"success":true,"extracted_text":"A"}`

	out, err := normalizeVisionToolOutput(raw, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != raw {
		t.Fatalf("expected raw output, got: %q", out)
	}
}
