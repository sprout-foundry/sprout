package llm

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --- API Key Management ---

func GetAPIKey(provider string) (string, error) {
	apiKeys, err := loadAPIKeys()
	if err != nil {
		fmt.Printf("Error loading API keys: %v\n", err)
		return "", err
	}

	apiKey, exists := apiKeys[provider]
	if !exists || apiKey == "" { // Also check if it's empty
		apiKey, err = promptForAPIKey(provider)
		if err != nil {
			fmt.Printf("Error prompting for API key: %v\n", err)
			return "", err
		}
		if apiKey != "" {
			apiKeys[provider] = apiKey
			if err := saveAPIKeys(apiKeys); err != nil {
				fmt.Printf("Error saving API keys: %v\n", err)
				return "", err
			}
		}
	}

	return apiKey, nil
}

func loadAPIKeys() (map[string]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting user home directory: %v\n", err)
		return nil, err
	}
	apiKeysPath := filepath.Join(home, ".ledit", "api_keys.json")

	data, err := os.ReadFile(apiKeysPath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		fmt.Printf("Error reading API keys file: %v\n", err)
		return nil, err
	}

	var apiKeys map[string]string
	if err := json.Unmarshal(data, &apiKeys); err != nil {
		fmt.Printf("Error unmarshaling API keys: %v\n", err)
		return nil, err
	}
	return apiKeys, nil
}

func saveAPIKeys(apiKeys map[string]string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting user home directory: %v\n", err)
		return err
	}
	apiKeysPath := filepath.Join(home, ".ledit", "api_keys.json")

	dir := filepath.Dir(apiKeysPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Error creating directory for API keys file: %v\n", err)
		return err
	}

	data, err := json.MarshalIndent(apiKeys, "", "    ")
	if err != nil {
		fmt.Printf("Error marshaling API keys: %v\n", err)
		return err
	}

	if err := os.WriteFile(apiKeysPath, data, 0644); err != nil {
		fmt.Printf("Error writing API keys file: %v\n", err)
		return err
	}

	return nil
}

func promptForAPIKey(provider string) (string, error) {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Enter the API key for %s (or press Enter to skip): ", provider)
	apiKey, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading input for API key: %v\n", err)
		return "", err
	}
	return strings.TrimSpace(apiKey), nil
}

func removeThinkTags(content string) string {
	var thinkTagPattern = regexp.MustCompile(`<think>[\s\S]*?<\/think>`)

	return thinkTagPattern.ReplaceAllString(content, "")
}
