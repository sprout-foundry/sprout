package agent

import (
	"context"
	"strings"
	"testing"
)

func TestHandleAskUserMissingQuestionV2(t *testing.T) {
	_, err := handleAskUser(context.Background(), nil, map[string]interface{}{})
	if err == nil {
		t.Error("expected error when question parameter is missing")
	}
	if err != nil && !strings.Contains(err.Error(), "missing 'question' parameter") {
		t.Errorf("expected 'missing question' error, got: %v", err)
	}
}

func TestHandleAskUserQuestionNotStringV2(t *testing.T) {
	_, err := handleAskUser(context.Background(), nil, map[string]interface{}{
		"question": 123,
	})
	if err == nil {
		t.Error("expected error when question is not a string")
	}
	if err != nil && !strings.Contains(err.Error(), "'question' parameter must be a string") {
		t.Errorf("expected 'must be a string' error, got: %v", err)
	}
}

func TestHandleAskUserQuestionIsArrayV2(t *testing.T) {
	_, err := handleAskUser(context.Background(), nil, map[string]interface{}{
		"question": []string{"not", "a", "string"},
	})
	if err == nil {
		t.Error("expected error when question is an array instead of string")
	}
}

func TestHandleAskUserQuestionIsMapV2(t *testing.T) {
	_, err := handleAskUser(context.Background(), nil, map[string]interface{}{
		"question": map[string]interface{}{"key": "value"},
	})
	if err == nil {
		t.Error("expected error when question is a map instead of string")
	}
}
