package mcp

// http_client_resources.go — resource + prompt operations on the HTTP MCP
// client, split out of http_client.go. ListResources / ReadResource cover the
// resources/* methods; ListPrompts / GetPrompt cover the prompts/* methods.
// Each mirrors the shared "send request → decode Result" pattern of the
// core client methods.
import (
	"context"
	"encoding/json"
	"fmt"
)

// ListResources lists available resources from the server
func (c *MCPHTTPClient) ListResources(ctx context.Context) ([]MCPResource, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	response, err := c.sendRequest(ctx, "resources/list", nil)
	if err != nil {
		return nil, fmt.Errorf("resources/list request failed: %w", err)
	}

	var resources []MCPResource
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		resourcesData, ok := resultMap["resources"]
		if !ok {
			return nil, fmt.Errorf("resources field not found in response")
		}

		resourcesBytes, err := json.Marshal(resourcesData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal resources data: %w", err)
		}

		if err := json.Unmarshal(resourcesBytes, &resources); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resources: %w", err)
		}

		// Set server name for all resources
		for i := range resources {
			resources[i].ServerName = c.config.Name
		}
	}

	return resources, nil
}

// ReadResource reads a resource from the server
func (c *MCPHTTPClient) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	params := map[string]interface{}{
		"uri": uri,
	}

	response, err := c.sendRequest(ctx, "resources/read", params)
	if err != nil {
		return nil, fmt.Errorf("resources/read request failed: %w", err)
	}

	var contents []MCPContent
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		contentsData, ok := resultMap["contents"]
		if !ok {
			return nil, fmt.Errorf("contents field not found in response")
		}

		contentsBytes, err := json.Marshal(contentsData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal contents data: %w", err)
		}

		if err := json.Unmarshal(contentsBytes, &contents); err != nil {
			return nil, fmt.Errorf("failed to unmarshal contents: %w", err)
		}
	}

	if len(contents) == 0 {
		return nil, fmt.Errorf("no content returned")
	}

	return &contents[0], nil
}

// ListPrompts lists available prompts from the server
func (c *MCPHTTPClient) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	response, err := c.sendRequest(ctx, "prompts/list", nil)
	if err != nil {
		return nil, fmt.Errorf("prompts/list request failed: %w", err)
	}

	var prompts []MCPPrompt
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		promptsData, ok := resultMap["prompts"]
		if !ok {
			return nil, fmt.Errorf("prompts field not found in response")
		}

		promptsBytes, err := json.Marshal(promptsData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal prompts data: %w", err)
		}

		if err := json.Unmarshal(promptsBytes, &prompts); err != nil {
			return nil, fmt.Errorf("failed to unmarshal prompts: %w", err)
		}

		// Set server name for all prompts
		for i := range prompts {
			prompts[i].ServerName = c.config.Name
		}
	}

	return prompts, nil
}

// GetPrompt gets a prompt from the server
func (c *MCPHTTPClient) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	c.mu.RLock()
	if !c.running || !c.initialized {
		c.mu.RUnlock()
		return nil, fmt.Errorf("client not started or initialized")
	}
	c.mu.RUnlock()

	params := map[string]interface{}{
		"name":      name,
		"arguments": args,
	}

	response, err := c.sendRequest(ctx, "prompts/get", params)
	if err != nil {
		return nil, fmt.Errorf("prompts/get request failed: %w", err)
	}

	var messages []MCPContent
	if response.Result != nil {
		resultMap, ok := response.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result type")
		}

		messagesData, ok := resultMap["messages"]
		if !ok {
			return nil, fmt.Errorf("messages field not found in response")
		}

		messagesBytes, err := json.Marshal(messagesData)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal messages data: %w", err)
		}

		if err := json.Unmarshal(messagesBytes, &messages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal messages: %w", err)
		}
	}

	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages returned")
	}

	return &messages[0], nil
}
