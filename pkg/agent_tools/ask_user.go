package tools

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// AskUser prompts the user with a question and reads input from stdin.
func AskUser(question string) (string, error) {
	if question == "" {
		return "", fmt.Errorf("empty question provided")
	}
	// Display the prompt
	fmt.Printf("%s: ", question)
	// Read user input
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read user input: %w", err)
	}
	// Trim whitespace and newline characters
	answer = strings.TrimSpace(answer)
	return answer, nil
}
