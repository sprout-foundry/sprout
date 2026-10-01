package agent

// scripted_stream.go — the scripted streaming client method, split out of
// scripted_assert.go. SendChatRequestStream simulates a streaming chat
// response (chunked playback, chunk errors, per-chunk delays, TPS tracking)
// on top of the ScriptedClient state defined in scripted_*.go.
import (
	"context"
	"errors"
	"fmt"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SendChatRequestStream sends a streaming chat request with full simulation support
func (c *ScriptedClient) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	c.mu.Lock()

	msgCopy := append([]api.Message(nil), messages...)
	c.sentRequests = append(c.sentRequests, msgCopy)

	if c.rateLimitExceeded {
		c.mu.Unlock()
		c.debugLog("Rate limit exceeded after %d attempts", c.rateLimitCounter)
		return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: errors.New("rate limit exceeded")}
	}

	var resp *ScriptedResponse
	if c.responses != nil && c.index < len(c.responses) {
		resp = c.responses[c.index]
	}

	if resp != nil && resp.RateLimitAfter > 0 {
		c.rateLimitThreshold = resp.RateLimitAfter
	}
	if c.rateLimitThreshold > 0 {
		c.rateLimitCounter++
		if c.rateLimitCounter >= c.rateLimitThreshold {
			c.rateLimitExceeded = true
			c.mu.Unlock()
			c.debugLog("Rate limit triggered after %d responses", c.rateLimitCounter)
			return nil, &RateLimitExceededError{Attempts: c.rateLimitCounter, LastError: errors.New("rate limit exceeded")}
		}
	}

	var content string
	var finishReason string
	var reasoningContent string
	var images []api.ImageData
	var toolCalls []api.ToolCall

	if resp != nil {
		content = resp.Content
		finishReason = resp.FinishReason
		reasoningContent = resp.ReasoningContent
		images = resp.Images
		toolCalls = resp.ToolCalls
	} else {
		content = "Test response from mock provider"
		finishReason = "stop"
	}

	c.mu.Unlock()

	if resp != nil && resp.Error != nil {
		c.debugLog("Returning injected error: %v", resp.Error)
		c.advanceIndex(resp)
		return nil, resp.Error
	}

	if resp != nil && resp.Delay > 0 {
		c.debugLog("Applying delay of %v", resp.Delay)
		select {
		case <-time.After(resp.Delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.ctx.Done():
			return nil, c.ctx.Err()
		}
	}

	// Handle streaming configuration
	if resp != nil && resp.StreamConfig != nil {
		streamConfig := resp.StreamConfig

		if len(streamConfig.Chunks) == 0 {
			c.advanceIndex(resp)
			return nil, errors.New("ScriptedResponse.StreamConfig.Chunks must not be empty")
		}
		if streamConfig.ChunkErrors != nil && len(streamConfig.ChunkErrors) > len(streamConfig.Chunks) {
			c.advanceIndex(resp)
			return nil, errors.New("ScriptedResponse.StreamConfig.ChunkErrors length exceeds number of chunks")
		}

		totalTokens := 0
		startTime := time.Now()

		for i, chunk := range streamConfig.Chunks {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-c.ctx.Done():
				return nil, c.ctx.Err()
			default:
			}

			callback(chunk, "assistant_text")

			if i < len(streamConfig.ChunkErrors) && streamConfig.ChunkErrors[i] != nil {
				c.debugLog("Chunk %d error: %v", i, streamConfig.ChunkErrors[i])
				c.advanceIndex(resp)
				return nil, streamConfig.ChunkErrors[i]
			}

			if streamConfig.ErrorAfterChunks > 0 && i >= streamConfig.ErrorAfterChunks-1 {
				c.debugLog("Error after %d chunks", streamConfig.ErrorAfterChunks)
				c.advanceIndex(resp)
				if streamConfig.StreamError != nil {
					return nil, streamConfig.StreamError
				}
				return nil, agenterrors.NewAgent("scripted_playback", fmt.Sprintf("simulated stream error after %d chunks", streamConfig.ErrorAfterChunks), nil)
			}

			if streamConfig.ChunkDelay > 0 && i < len(streamConfig.Chunks)-1 {
				select {
				case <-time.After(streamConfig.ChunkDelay):
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-c.ctx.Done():
					return nil, c.ctx.Err()
				}
			}

			if streamConfig.ChunkDelay > 0 && streamConfig.TokensPerChunk > 0 {
				chunkTPS := float64(streamConfig.TokensPerChunk) / streamConfig.ChunkDelay.Seconds()
				c.mu.Lock()
				c.lastTPS = chunkTPS
				c.averageTPS = (c.averageTPS + chunkTPS) / 2
				c.mu.Unlock()
			}

			totalTokens += streamConfig.TokensPerChunk

			c.debugLog("Streamed chunk %d (%d tokens, %.2f TPS)", i+1, streamConfig.TokensPerChunk, c.lastTPS)
		}

		finishReason = streamConfig.FinishReason

		if streamConfig.ChunkDelay > 0 && len(streamConfig.Chunks) > 0 {
			totalTime := time.Since(startTime)
			if totalTime > 0 {
				finalTPS := float64(totalTokens) / totalTime.Seconds()
				c.mu.Lock()
				c.lastTPS = finalTPS
				c.mu.Unlock()
			}
		}
	} else {
		callback(content, "assistant_text")
	}

	response := c.buildChatResponse(
		"scripted-response-",
		c.GetModel(),
		content,
		finishReason,
		reasoningContent,
		images,
		toolCalls,
		resolveUsage(resp),
	)

	c.advanceIndex(resp)

	c.debugLog("Consumed streaming response at index %d", c.GetIndex()-1)

	return response, nil
}
