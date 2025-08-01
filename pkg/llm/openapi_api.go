package llm

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/prompts"
)

func callOpenAICompatibleStream(apiURL, apiKey, model string, messages []prompts.Message, timeout time.Duration, writer io.Writer) error {
	reqBody, err := json.Marshal(OpenAIRequest{
		Model:       model,
		Messages:    messages,
		Temperature: 0.0,
		Stream:      true,
	})
	if err != nil {
		fmt.Print(prompts.RequestMarshalError(err)) // Use prompt
		return err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(reqBody))
	if err != nil {
		fmt.Print(prompts.RequestCreationError(err)) // Use prompt
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Print(prompts.HTTPRequestError(err)) // Use prompt
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Print(prompts.APIError(string(body), resp.StatusCode)) // Use prompt
		return fmt.Errorf(prompts.APIError(string(body), resp.StatusCode))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			line = line[6:]
		}
		if line == "[DONE]" {
			break
		}

		var openAIResp OpenAIResponse
		if err := json.Unmarshal([]byte(line), &openAIResp); err != nil {
			// Don't print error for every line, just continue
			continue
		}

		if len(openAIResp.Choices) > 0 {
			content := openAIResp.Choices[0].Delta.Content
			if _, err := writer.Write([]byte(content)); err != nil {
				return err
			}
		}
	}

	if err := scanner.Err(); err != nil {
		fmt.Print(prompts.ResponseBodyError(err)) // Use prompt
		return err
	}

	return nil
}
