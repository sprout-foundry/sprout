package agent

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// displayIntermediateResponse shows intermediate assistant responses (during tool execution)
func (ch *ConversationHandler) displayIntermediateResponse(content string) {
	content = strings.TrimSpace(content)
	if len(content) > 0 {
		if ch.agent.streamingEnabled {
			// During streaming, content has already been displayed in real-time
			// But we need to ensure proper spacing and formatting after tool calls
			// Add a newline to separate from tool execution output
			fmt.Printf("\n")
		} else {
			// Display thinking message for non-streaming mode
			// In CI mode, don't use cursor control sequences
			if os.Getenv("LEDIT_CI_MODE") == "1" || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
				fmt.Printf("💭 %s\n", content)
			} else {
				fmt.Printf("\r\033[K💭 %s\n", content)
			}
		}
	}
}

// displayFinalResponse shows the final assistant response
func (ch *ConversationHandler) displayFinalResponse(content string) {
	if !ch.agent.streamingEnabled {
		fmt.Printf("%s\n", content)
	}
}

// displayUserFriendlyError shows contextual error messages to the user
func (ch *ConversationHandler) displayUserFriendlyError(err error) {
	errStr := err.Error()
	providerName := cases.Title(language.Und).String(ch.agent.GetProvider())

	var userMessage string

	// Categorize errors for better user experience
	if strings.Contains(errStr, "timed out") {
		if strings.Contains(errStr, "no response received") {
			userMessage = fmt.Sprintf("⏰ %s is taking longer than usual to respond. This might be due to high load or network issues.\n💡 Try again in a few moments, or use a simpler query if the problem persists.", providerName)
		} else if strings.Contains(errStr, "no data received") {
			userMessage = fmt.Sprintf("⏰ %s stopped sending data. The connection may have been interrupted.\n💡 Please try your request again.", providerName)
		} else {
			userMessage = fmt.Sprintf("⏰ %s request timed out. This usually indicates network issues or high server load.\n💡 Try again in a few moments, or break your request into smaller parts.", providerName)
		}
	} else if strings.Contains(errStr, "connection") || strings.Contains(errStr, "network") {
		userMessage = fmt.Sprintf("🔌 Connection to %s failed. Please check your internet connection and try again.", providerName)
	} else if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") {
		userMessage = fmt.Sprintf("🚦 %s rate limit reached. Please wait a moment before trying again.", providerName)
	} else if strings.Contains(errStr, "401") || strings.Contains(errStr, "unauthorized") {
		userMessage = fmt.Sprintf("🔑 %s API key issue. Please check your authentication.", providerName)
	} else if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") || strings.Contains(errStr, "503") {
		userMessage = fmt.Sprintf("🔧 %s is experiencing server issues. Please try again in a few minutes.", providerName)
	} else {
		userMessage = fmt.Sprintf("❌ %s API error: %v", providerName, err)
	}

	// Display the message in the content area via agent routing
	ch.agent.PrintLine("")
	ch.agent.PrintLine(userMessage)
	ch.agent.PrintLine("")
}
