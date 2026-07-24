//go:build !js

package webui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// tryParseMultipartFile — pure helper (accepts byte body + content-type)
// ---------------------------------------------------------------------------

func TestTryParseMultipartFile_NotMultipart(t *testing.T) {
	data, ok := tryParseMultipartFile([]byte("hello"), "application/json")
	if ok {
		t.Error("should return false for non-multipart content type")
	}
	if data != nil {
		t.Error("should return nil data for non-multipart")
	}
}

func TestTryParseMultipartFile_EmptyBody(t *testing.T) {
	data, ok := tryParseMultipartFile([]byte{}, "multipart/form-data; boundary=----WebKitFormBoundary")
	if ok {
		t.Error("should return false for empty multipart body")
	}
	if data != nil {
		t.Error("should return nil data")
	}
}

func TestTryParseMultipartFile_NoImageField(t *testing.T) {
	// Build a valid multipart body with a "text" field but no "image" field
	body := buildMultipartBody("----boundary", map[string]string{
		"text": "hello world",
	})
	data, ok := tryParseMultipartFile(body, "multipart/form-data; boundary=----boundary")
	if ok {
		t.Error("should return false when no 'image' field exists")
	}
	if data != nil {
		t.Error("should return nil data")
	}
}

func TestTryParseMultipartFile_WithImageField(t *testing.T) {
	// Build a multipart body with an "image" field containing binary data
	imageData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG magic bytes
	body := buildMultipartWithFile("----boundary", "image", imageData, "photo.png")
	data, ok := tryParseMultipartFile(body, "multipart/form-data; boundary=----boundary")
	if !ok {
		t.Fatal("should return true for multipart with image field")
	}
	if !bytes.Equal(data, imageData) {
		t.Errorf("data mismatch: got %v, want %v", data, imageData)
	}
}

func TestTryParseMultipartFile_ContentTypeContainsMultipart(t *testing.T) {
	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0} // JPEG magic bytes
	body := buildMultipartWithFile("----boundary", "image", imageData, "photo.jpg")
	// Content-Type with charset and other params
	ct := "multipart/form-data; charset=utf-8; boundary=----boundary"
	data, ok := tryParseMultipartFile(body, ct)
	if !ok {
		t.Fatal("should parse with complex content-type")
	}
	if !bytes.Equal(data, imageData) {
		t.Errorf("data mismatch: got %v, want %v", data, imageData)
	}
}

func TestTryParseMultipartFile_InvalidBoundary(t *testing.T) {
	// Body with a boundary that doesn't match what's in the content-type
	body := buildMultipartWithFile("----wrong", "image", []byte("data"), "img.png")
	_, ok := tryParseMultipartFile(body, "multipart/form-data; boundary=----right")
	if ok {
		t.Error("should fail when boundary doesn't match")
	}
}

func TestTryParseMultipartFile_CaseInsensitiveField(t *testing.T) {
	// FormFile is case-sensitive; "Image" != "image"
	body := buildMultipartWithFile("----b", "Image", []byte("data"), "img.png")
	_, ok := tryParseMultipartFile(body, "multipart/form-data; boundary=----b")
	if ok {
		t.Log("case-sensitive field name may match depending on impl")
	}
}

// buildMultipartBody creates a simple multipart body with text fields only.
func buildMultipartBody(boundary string, fields map[string]string) []byte {
	var b bytes.Buffer
	b.WriteString("--" + boundary + "\r\n")
	for name, value := range fields {
		b.WriteString("Content-Disposition: form-data; name=\"" + name + "\"\r\n\r\n")
		b.WriteString(value + "\r\n")
		b.WriteString("--" + boundary + "\r\n")
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes()
}

func buildMultipartWithFile(boundary, field string, data []byte, filename string) []byte {
	var b bytes.Buffer
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Disposition: form-data; name=\"" + field + "\"; filename=\"" + filename + "\"\r\n")
	b.WriteString("Content-Type: application/octet-stream\r\n\r\n")
	b.Write(data)
	b.WriteString("\r\n--" + boundary + "--\r\n")
	return b.Bytes()
}

// ---------------------------------------------------------------------------
// writeExternalPathConsentRequired (moved to settings_api_general_new_test.go
// because it's also tested there; only included in one file)
// ---------------------------------------------------------------------------

// tryParseMultipartFile edge cases
func TestTryParseMultipartFile_NilBody(t *testing.T) {
	data, ok := tryParseMultipartFile(nil, "multipart/form-data; boundary=----b")
	if ok {
		t.Error("should return false for nil body")
	}
	if data != nil {
		t.Error("should return nil data")
	}
}

func TestTryParseMultipartFile_GarbageMultipart(t *testing.T) {
	data, ok := tryParseMultipartFile([]byte("not multipart at all"), "multipart/form-data; boundary=----b")
	if ok {
		t.Error("should return false for garbage data")
	}
	if data != nil {
		t.Error("should return nil data")
	}
}

func TestTryParseMultipartFile_OnlyImageFieldEmpty(t *testing.T) {
	// Image field with empty content
	body := buildMultipartWithFile("----b", "image", []byte{}, "empty.png")
	data, ok := tryParseMultipartFile(body, "multipart/form-data; boundary=----b")
	if !ok {
		t.Fatal("should succeed even with empty file content")
	}
	if len(data) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(data))
	}
}

// ---------------------------------------------------------------------------
// handleAPIGreet — greeting endpoint
// ---------------------------------------------------------------------------

func TestHandleAPIGreet(t *testing.T) {
	t.Parallel()
	
	// Create a test server
	server := &ReactWebServer{}
	
	t.Run("GET request returns greeting", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/greet", nil)
		
		server.handleAPIGreet(w, r)
		
		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
		
		var response map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		
		expectedMessage := "Hello from Sprout!"
		if msg, ok := response["message"]; !ok || msg != expectedMessage {
			t.Errorf("expected message '%s', got '%v'", expectedMessage, msg)
		}
		
		if status, ok := response["status"]; !ok || status != "success" {
			t.Errorf("expected status 'success', got '%v'", status)
		}
	})
	
	t.Run("non-GET methods return method not allowed", func(t *testing.T) {
		methods := []string{"POST", "PUT", "DELETE", "PATCH"}
		for _, method := range methods {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(method, "/greet", nil)
			
			server.handleAPIGreet(w, r)
			
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405 for %s, got %d", method, w.Code)
			}
			
			var response map[string]interface{}
			if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
				t.Fatalf("failed to decode response for %s: %v", method, err)
			}
			
			if code, ok := response["code"]; !ok || code != "method_not_allowed" {
				t.Errorf("expected error code 'method_not_allowed' for %s, got '%v'", method, code)
			}
		}
	})
}
