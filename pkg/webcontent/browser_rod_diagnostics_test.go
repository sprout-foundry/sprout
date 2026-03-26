//go:build browser

package webcontent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectCORSIssuesAndMarkRequests(t *testing.T) {
	requests := []NetworkRequest{
		{Type: "fetch", URL: "https://api.example.com/data", Method: "GET", Error: "Access to fetch at 'https://api.example.com/data' from origin 'http://localhost:3000' has been blocked by CORS policy"},
		{Type: "xhr", URL: "https://api.example.com/ok", Method: "GET", Status: 200, OK: true},
	}

	marked := markCORSBlockedRequests(requests)
	require.Len(t, marked, 2)
	assert.True(t, marked[0].CORSBlocked)
	assert.False(t, marked[1].CORSBlocked)

	issues := detectCORSIssues(
		[]string{"Access to fetch at 'https://api.example.com/data' from origin 'http://localhost:3000' has been blocked by CORS policy"},
		[]string{"Cross-Origin Request Blocked: The Same Origin Policy disallows reading the remote resource"},
		marked,
	)
	require.NotEmpty(t, issues)
	assert.Contains(t, issues[0], "CORS")
}
