package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// parseGitHubRemoteURL
// ---------------------------------------------------------------------------

func TestParseGitHubRemoteURL_SSH(t *testing.T) {
	info := parseGitHubRemoteURL("git@github.com:owner/repo")
	require := assert.New(t)
	require.NotNil(info)
	assert.Equal(t, "owner", info.Owner)
	assert.Equal(t, "repo", info.Repo)
	assert.Equal(t, "https://github.com/owner/repo", info.URL)
}

func TestParseGitHubRemoteURL_SSH_WithGitExt(t *testing.T) {
	info := parseGitHubRemoteURL("git@github.com:owner/repo.git")
	require := assert.New(t)
	require.NotNil(info)
	assert.Equal(t, "owner", info.Owner)
	assert.Equal(t, "repo", info.Repo)
}

func TestParseGitHubRemoteURL_HTTPS(t *testing.T) {
	info := parseGitHubRemoteURL("https://github.com/owner/repo")
	require := assert.New(t)
	require.NotNil(info)
	assert.Equal(t, "owner", info.Owner)
	assert.Equal(t, "repo", info.Repo)
	assert.Equal(t, "https://github.com/owner/repo", info.URL)
}

func TestParseGitHubRemoteURL_HTTPS_WithGitExt(t *testing.T) {
	info := parseGitHubRemoteURL("https://github.com/owner/repo.git")
	require := assert.New(t)
	require.NotNil(info)
	assert.Equal(t, "owner", info.Owner)
	assert.Equal(t, "repo", info.Repo)
}

func TestParseGitHubRemoteURL_HTTP(t *testing.T) {
	info := parseGitHubRemoteURL("http://github.com/owner/repo")
	require := assert.New(t)
	require.NotNil(info)
	assert.Equal(t, "owner", info.Owner)
	assert.Equal(t, "repo", info.Repo)
	assert.Equal(t, "https://github.com/owner/repo", info.URL)
}

func TestParseGitHubRemoteURL_TrailingSlash(t *testing.T) {
	info := parseGitHubRemoteURL("https://github.com/owner/repo/")
	require := assert.New(t)
	require.NotNil(info)
	assert.Equal(t, "owner", info.Owner)
	assert.Equal(t, "repo", info.Repo)
}

func TestParseGitHubRemoteURL_NotGitHub(t *testing.T) {
	info := parseGitHubRemoteURL("git@gitlab.com:owner/repo")
	assert.Nil(t, info)
}

func TestParseGitHubRemoteURL_Empty(t *testing.T) {
	info := parseGitHubRemoteURL("")
	assert.Nil(t, info)
}

func TestParseGitHubRemoteURL_Malformed(t *testing.T) {
	info := parseGitHubRemoteURL("https://github.com/")
	assert.Nil(t, info)
}

// ---------------------------------------------------------------------------
// IsGitHubMCPConfigured
// ---------------------------------------------------------------------------

func TestIsGitHubMCPConfigured_WithCommand(t *testing.T) {
	cfg := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"github": {Name: "github", Command: "npx"},
		},
	}
	assert.True(t, IsGitHubMCPConfigured(cfg))
}

func TestIsGitHubMCPConfigured_WithURL(t *testing.T) {
	cfg := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"github": {Name: "github", URL: "https://api.githubcopilot.com/mcp/"},
		},
	}
	assert.True(t, IsGitHubMCPConfigured(cfg))
}

func TestIsGitHubMCPConfigured_NotConfigured(t *testing.T) {
	cfg := MCPConfig{Servers: map[string]MCPServerConfig{}}
	assert.False(t, IsGitHubMCPConfigured(cfg))
}

func TestIsGitHubMCPConfigured_EmptyServer(t *testing.T) {
	cfg := MCPConfig{
		Servers: map[string]MCPServerConfig{
			"github": {Name: "github"},
		},
	}
	assert.False(t, IsGitHubMCPConfigured(cfg))
}
