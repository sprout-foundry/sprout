package factory

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// TestTestClient_SendChatRequest tests the TestClient's SendChatRequest method
func TestTestClient_SendChatRequest(t *testing.T) {
	client := &TestClient{model: "test-model"}

	messages := []api.Message{
		{Role: "user", Content: "Hello"},
	}
	tools := []api.Tool{}

	resp, err := client.SendChatRequest(messages, tools, "")
	if err != nil {
		t.Fatalf("SendChatRequest failed: %v", err)
	}

	if resp.ID != "test-response-id" {
		t.Errorf("Expected ID 'test-response-id', got '%s'", resp.ID)
	}

	if resp.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", resp.Model)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(resp.Choices))
	}

	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", resp.Choices[0].Message.Role)
	}

	if resp.Choices[0].Message.Content != "Test response from mock provider" {
		t.Errorf("Unexpected content: '%s'", resp.Choices[0].Message.Content)
	}

	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Expected 15 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

// TestTestClient_SendChatRequestStream tests the streaming method
func TestTestClient_SendChatRequestStream(t *testing.T) {
	client := &TestClient{model: "test-model"}

	var receivedChunks []string
	callback := func(chunk string) {
		receivedChunks = append(receivedChunks, chunk)
	}

	messages := []api.Message{
		{Role: "user", Content: "Hello"},
	}

	resp, err := client.SendChatRequestStream(messages, nil, "", callback)
	if err != nil {
		t.Fatalf("SendChatRequestStream failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response should not be nil")
	}

	if len(receivedChunks) != 1 {
		t.Fatalf("Expected 1 chunk, got %d", len(receivedChunks))
	}

	if receivedChunks[0] != "Test response from mock provider" {
		t.Errorf("Unexpected chunk content: '%s'", receivedChunks[0])
	}
}

// TestTestClient_CheckConnection tests the CheckConnection method
func TestTestClient_CheckConnection(t *testing.T) {
	client := &TestClient{}

	err := client.CheckConnection()
	if err != nil {
		t.Errorf("CheckConnection should always return nil for test client, got: %v", err)
	}
}

// TestTestClient_SetDebug tests the SetDebug method
func TestTestClient_SetDebug(t *testing.T) {
	client := &TestClient{debug: false}

	client.SetDebug(true)
	if !client.debug {
		t.Error("Expected debug to be true")
	}

	client.SetDebug(false)
	if client.debug {
		t.Error("Expected debug to be false")
	}
}

// TestTestClient_SetModel tests the SetModel method
func TestTestClient_SetModel(t *testing.T) {
	client := &TestClient{}

	err := client.SetModel("new-model")
	if err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

	if client.model != "new-model" {
		t.Errorf("Expected model 'new-model', got '%s'", client.model)
	}
}

// TestTestClient_GetModel tests the GetModel method
func TestTestClient_GetModel(t *testing.T) {
	// Test with model set
	client := &TestClient{model: "custom-model"}
	if client.GetModel() != "custom-model" {
		t.Errorf("Expected 'custom-model', got '%s'", client.GetModel())
	}

	// Test with empty model (should return default)
	client = &TestClient{model: ""}
	if client.GetModel() != "test-model" {
		t.Errorf("Expected default 'test-model', got '%s'", client.GetModel())
	}
}

// TestTestClient_GetProvider tests the GetProvider method
func TestTestClient_GetProvider(t *testing.T) {
	client := &TestClient{}

	if client.GetProvider() != "test" {
		t.Errorf("Expected provider 'test', got '%s'", client.GetProvider())
	}
}

// TestTestClient_GetModelContextLimit tests the GetModelContextLimit method
func TestTestClient_GetModelContextLimit(t *testing.T) {
	client := &TestClient{}

	limit, err := client.GetModelContextLimit()
	if err != nil {
		t.Fatalf("GetModelContextLimit failed: %v", err)
	}

	if limit != 4096 {
		t.Errorf("Expected context limit 4096, got %d", limit)
	}
}

// TestTestClient_ListModels tests the ListModels method
func TestTestClient_ListModels(t *testing.T) {
	client := &TestClient{}

	models, err := client.ListModels()
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	if models[0].Name != "test-model" {
		t.Errorf("Expected model name 'test-model', got '%s'", models[0].Name)
	}

	if models[0].ContextLength != 4096 {
		t.Errorf("Expected context length 4096, got %d", models[0].ContextLength)
	}
}

// TestTestClient_SupportsVision tests the SupportsVision method
func TestTestClient_SupportsVision(t *testing.T) {
	client := &TestClient{}

	if client.SupportsVision() {
		t.Error("TestClient should not support vision")
	}
}

// TestTestClient_GetVisionModel tests the GetVisionModel method
func TestTestClient_GetVisionModel(t *testing.T) {
	client := &TestClient{}

	if client.GetVisionModel() != "" {
		t.Errorf("Expected empty vision model, got '%s'", client.GetVisionModel())
	}
}

// TestTestClient_SendVisionRequest tests that vision requests return an error
func TestTestClient_SendVisionRequest(t *testing.T) {
	client := &TestClient{}

	_, err := client.SendVisionRequest(nil, nil, "")
	if err == nil {
		t.Error("SendVisionRequest should return an error for test client")
	}

	expectedErr := "vision not supported in test provider"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

// TestTestClient_TPSStats tests all TPS-related methods
func TestTestClient_TPSStats(t *testing.T) {
	client := &TestClient{}

	// Test GetLastTPS
	lastTPS := client.GetLastTPS()
	if lastTPS != 100.0 {
		t.Errorf("Expected last TPS 100.0, got %f", lastTPS)
	}

	// Test GetAverageTPS
	avgTPS := client.GetAverageTPS()
	if avgTPS != 100.0 {
		t.Errorf("Expected average TPS 100.0, got %f", avgTPS)
	}

	// Test GetTPSStats
	stats := client.GetTPSStats()
	if stats["last"] != 100.0 {
		t.Errorf("Expected stats['last'] 100.0, got %f", stats["last"])
	}
	if stats["average"] != 100.0 {
		t.Errorf("Expected stats['average'] 100.0, got %f", stats["average"])
	}

	// ResetTPSStats should be a no-op and not panic
	client.ResetTPSStats()

	// Verify stats are unchanged after reset (since it's a no-op)
	stats = client.GetTPSStats()
	if stats["last"] != 100.0 {
		t.Errorf("Expected stats['last'] 100.0 after reset, got %f", stats["last"])
	}
}

// TestCreateProviderClient_TestClientType tests creating a TestClient via the factory
func TestCreateProviderClient_TestClientType(t *testing.T) {
	client, err := CreateProviderClient(api.TestClientType, "test-model")
	if err != nil {
		t.Fatalf("CreateProviderClient failed for TestClientType: %v", err)
	}

	// Verify it's a TestClient
	_, ok := client.(*TestClient)
	if !ok {
		t.Error("Expected TestClient type")
	}

	// Verify model is set
	if client.GetModel() != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", client.GetModel())
	}
}

// TestCreateProviderClient_TestClientType_EmptyModel tests creating a TestClient without specifying a model
func TestCreateProviderClient_TestClientType_EmptyModel(t *testing.T) {
	client, err := CreateProviderClient(api.TestClientType, "")
	if err != nil {
		t.Fatalf("CreateProviderClient failed: %v", err)
	}

	// Should return default model
	if client.GetModel() != "test-model" {
		t.Errorf("Expected default model 'test-model', got '%s'", client.GetModel())
	}
}

// TestCreateProviderClient_TestClientType_FullInterface tests that TestClient implements ClientInterface
func TestCreateProviderClient_TestClientType_FullInterface(t *testing.T) {
	client, err := CreateProviderClient(api.TestClientType, "test-model")
	if err != nil {
		t.Fatalf("CreateProviderClient failed: %v", err)
	}

	// Test all interface methods work without panic
	_ = client.GetProvider()
	_, _ = client.GetModelContextLimit()
	_, _ = client.ListModels()
	_ = client.SupportsVision()
	_ = client.GetVisionModel()
	_, _ = client.SendChatRequest(nil, nil, "")
	_, _ = client.SendVisionRequest(nil, nil, "")
	_ = client.GetLastTPS()
	_ = client.GetAverageTPS()
	_ = client.GetTPSStats()
	client.ResetTPSStats()
	client.SetDebug(true)
	_ = client.CheckConnection()
}

// TestTestClient_NilMessages tests that TestClient handles nil gracefully
func TestTestClient_NilMessages(t *testing.T) {
	client := &TestClient{}

	// Should not panic with nil inputs
	resp, err := client.SendChatRequest(nil, nil, "")
	if err != nil {
		t.Fatalf("SendChatRequest failed with nil inputs: %v", err)
	}

	if resp == nil {
		t.Error("Response should not be nil")
	}
}
