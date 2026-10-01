//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"golang.org/x/term"
)

type customKeySource int

const (
	keySourceAPIKey customKeySource = iota
	keySourceEnvVar
	keySourceNone
)

// readSecret reads one line from the terminal without echoing it, so a
// pasted API key never appears on screen or in scrollback. Swapped out by
// tests, which have no terminal.
var readSecret = func(prompt string) (string, error) {
	fmt.Print(prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

// promptKeySource asks how the provider's API key is supplied. A pasted key
// is the default: it lives in sprout's credential store (OS keyring or the
// encrypted credentials file) rather than in the environment of every
// process the shell starts.
func promptKeySource(reader *bufio.Reader) (customKeySource, error) {
	fmt.Println()
	fmt.Println("How should sprout get this provider's API key?")
	fmt.Println("  1) Paste the API key now — kept in sprout's credential store (recommended)")
	fmt.Println("  2) Read it from an environment variable")
	fmt.Println("  3) No API key (local or unauthenticated endpoint)")
	for {
		answer, err := promptLine(reader, "Choice [1]: ")
		if err != nil {
			return 0, err
		}
		switch strings.TrimSpace(answer) {
		case "", "1":
			return keySourceAPIKey, nil
		case "2":
			return keySourceEnvVar, nil
		case "3":
			return keySourceNone, nil
		}
		fmt.Println("Please enter 1, 2 or 3.")
	}
}

// configureKeySource runs the chosen key-source step and records it on
// provider. It returns the pasted key, if any, which the caller stores only
// after the provider itself has been saved.
func configureKeySource(reader *bufio.Reader, provider *configuration.CustomProviderConfig) (string, error) {
	source, err := promptKeySource(reader)
	if err != nil {
		return "", fmt.Errorf("failed to prompt for API key source: %w", err)
	}

	switch source {
	case keySourceEnvVar:
		envVar, err := promptEnvVarName(reader)
		if err != nil {
			return "", err
		}
		provider.EnvVar = envVar
		provider.RequiresAPIKey = true
		if strings.TrimSpace(os.Getenv(envVar)) != "" {
			fmt.Printf("(%s is set; discovery will use it)\n", envVar)
		} else {
			fmt.Printf("(%s is not set in this shell; set it before using the provider)\n", envVar)
		}
		return "", nil
	case keySourceNone:
		provider.EnvVar = ""
		provider.RequiresAPIKey = false
		return "", nil
	default:
		provider.EnvVar = ""
		provider.RequiresAPIKey = true
		key, err := readSecret("API key (input hidden): ")
		if err != nil {
			return "", fmt.Errorf("failed to read API key: %w", err)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			fmt.Printf("No key entered. Add it later with `sprout custom add %s`.\n", provider.Name)
		}
		return key, nil
	}
}

func promptEnvVarName(reader *bufio.Reader) (string, error) {
	for {
		name, err := promptLine(reader, "Environment variable name (e.g. MY_GATEWAY_API_KEY): ")
		if err != nil {
			return "", fmt.Errorf("failed to prompt for API key env var: %w", err)
		}
		if name = strings.TrimSpace(name); name != "" {
			return name, nil
		}
		fmt.Println("Enter a variable name, or re-run and pick another option.")
	}
}

// storePastedKey saves a key pasted during setup. The provider is already
// saved at this point, so a storage failure is reported, not fatal.
func storePastedKey(providerName, key string) {
	if key == "" {
		return
	}
	if err := credentials.SetToActiveBackend(providerName, key); err != nil {
		console.GlyphWarning.Printf("Provider saved, but the API key could not be stored: %v", err)
		fmt.Printf("Re-run `sprout custom add %s` to try again, or use an environment variable.\n", providerName)
		return
	}
	console.GlyphSuccess.Printf("API key stored in the credential store for %s", providerName)
}

// describeKeySource summarizes where a saved provider's API key comes from,
// without revealing the key.
func describeKeySource(name string, provider configuration.CustomProviderConfig) string {
	switch {
	case provider.EnvVar != "":
		return "env var " + provider.EnvVar
	case !provider.RequiresAPIKey:
		return "none"
	}
	if value, source, err := credentials.GetFromActiveBackend(name); err == nil && strings.TrimSpace(value) != "" {
		return "stored (" + source + ")"
	}
	return fmt.Sprintf("not set — run `sprout custom add %s` to add it", name)
}
