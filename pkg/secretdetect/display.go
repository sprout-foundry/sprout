package secretdetect

import "strings"

// displayNames maps the common gitleaks rule IDs to friendly user-facing
// labels. Anything not in this map falls back to a generic title-case with
// abbreviation expansion (see DisplayName).
var displayNames = map[string]string{
	"openai-api-key":          "OpenAI API Key",
	"anthropic-api-key":       "Anthropic API Key",
	"aws-access-token":        "AWS Access Key",
	"aws-secret-key":          "AWS Secret Key",
	"gcp-api-key":             "Google Cloud API Key",
	"github-pat":              "GitHub Personal Access Token",
	"github-fine-grained-pat": "GitHub Fine-Grained PAT",
	"github-oauth":            "GitHub OAuth Token",
	"github-app-token":        "GitHub App Token",
	"gitlab-pat":              "GitLab Personal Access Token",
	"jwt":                     "JWT",
	"private-key":             "Private Key",
	"slack-bot-token":         "Slack Bot Token",
	"slack-user-token":        "Slack User Token",
	"slack-app-token":         "Slack App Token",
	"slack-webhook-url":       "Slack Webhook URL",
	"stripe-access-token":     "Stripe API Key",
	"twilio-api-key":          "Twilio API Key",
	"generic-api-key":         "API Key",
}

// abbrevs are tokens that should be uppercased rather than title-cased when
// constructing a fallback display name from an unknown rule ID.
var abbrevs = map[string]string{
	"api":   "API",
	"aws":   "AWS",
	"gcp":   "GCP",
	"id":    "ID",
	"jwt":   "JWT",
	"oauth": "OAuth",
	"pat":   "PAT",
	"rsa":   "RSA",
	"ssh":   "SSH",
	"url":   "URL",
}

// DisplayName returns a user-facing label for a gitleaks rule ID. For known
// rules it returns a curated string; for unknown rules it title-cases the
// dash-separated segments and expands common abbreviations.
func DisplayName(ruleID string) string {
	if ruleID == "" {
		return "Secret"
	}
	if name, ok := displayNames[ruleID]; ok {
		return name
	}
	parts := strings.Split(ruleID, "-")
	for i, p := range parts {
		if up, ok := abbrevs[p]; ok {
			parts[i] = up
		} else if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}
