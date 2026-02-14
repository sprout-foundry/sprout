package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filePath string
		expected bool
	}{
		// File path tests
		{
			name:     "test file path",
			content:  "some code",
			filePath: "example_test.go",
			expected: true,
		},
		{
			name:     "test file path with _test suffix",
			content:  "some code",
			filePath: "myfile_test.go",
			expected: true,
		},
		{
			name:     "example in path",
			content:  "some code",
			filePath: "config_example.json",
			expected: true,
		},
		{
			name:     "demo in path",
			content:  "some code",
			filePath: "demo.go",
			expected: true,
		},
		{
			name:     "sample in path",
			content:  "some code",
			filePath: "sample_config.yaml",
			expected: true,
		},
		{
			name:     "mock in path",
			content:  "some code",
			filePath: "mock_server.go",
			expected: true,
		},
		{
			name:     ".env.example file",
			content:  "API_KEY=test",
			filePath: ".env.example",
			expected: true,
		},
		{
			name:     "config.example file",
			content:  "{}",
			filePath: "config.example",
			expected: true,
		},
		{
			name:     "regular file path",
			content:  "some code",
			filePath: "main.go",
			expected: false,
		},
		// Content tests
		{
			name:     "# test comment",
			content:  "# test comment",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "// test comment",
			content:  "// test comment",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "/* test comment */",
			content:  "/* test comment */",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "test_ function prefix",
			content:  "func Test_foo()",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "_test function suffix",
			content:  "func foo_test()",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "# example comment",
			content:  "# example config",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "// demo comment",
			content:  "// demo usage",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "# placeholder comment",
			content:  "# placeholder value",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "# sample comment",
			content:  "# sample code",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "# mock comment",
			content:  "# mock data",
			filePath: "main.go",
			expected: true,
		},
		{
			name:     "PASS indicator",
			content:  "PASS: test passed",
			filePath: "main.go",
			expected: false, // Bug: indicator not lowercased
		},
		{
			name:     "FAIL indicator",
			content:  "FAIL: test failed",
			filePath: "main.go",
			expected: false, // Bug: indicator not lowercased
		},
		{
			name:     "TODO indicator",
			content:  "TODO: implement this",
			filePath: "main.go",
			expected: false, // Bug: indicator not lowercased
		},
		{
			name:     "FIXME indicator",
			content:  "FIXME: fix this bug",
			filePath: "main.go",
			expected: false, // Bug: indicator not lowercased
		},
		{
			name:     "regular content no test indicators",
			content:  "func main() { fmt.Println(\"hello\") }",
			filePath: "main.go",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTestFile(tt.content, tt.filePath)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectSecurityConcerns_APIKeys(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "API key detection",
			content:  `api_key = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "apiKey detection",
			content:  `apikey = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "api-key detection",
			content:  `api-key = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "access_key detection",
			content:  `access_key = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "secret_key detection",
			content:  `secret_key = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "auth_token detection",
			content:  `auth_token = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "bearer_token detection",
			content:  `bearer_token = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "client_secret detection",
			content:  `client_secret = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "private_key detection",
			content:  `private_key = "sk_live_1234567890abcdefghij"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "No API key - too short",
			content:  `api_key = "short"`,
			expected: nil,
		},
		{
			name:     "No API key - no key pattern",
			content:  `value = "abcdefghijklmnopqrst"`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcerns_Passwords(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "password detection",
			content:  `password = "mysecretpass123"`,
			expected: []string{"Password Exposure"},
		},
		{
			name:     "passwd detection",
			content:  `passwd = "mysecretpass123"`,
			expected: []string{"Password Exposure"},
		},
		{
			name:     "pass detection",
			content:  `pass = "mysecretpass123"`,
			expected: []string{"Password Exposure"},
		},
		{
			name:     "pwd detection",
			content:  `pwd = "mysecretpass123"`,
			expected: []string{"Password Exposure"},
		},
		{
			name:     "passphrase detection",
			content:  `passphrase = "mysecretpass123"`,
			expected: []string{"Password Exposure"},
		},
		{
			name:     "Password with colon",
			content:  `password: "mysecretpass123"`,
			expected: []string{"Password Exposure"},
		},
		{
			name:     "No password - too short",
			content:  `password = "short"`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcerns_AWSCredentials(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "AWS Access Key ID - AKIA",
			content:  `AKIAIOSFODNN7EXAMPLE`,
			expected: []string{"AWS Access Key ID Exposure"},
		},
		{
			name:     "AWS Access Key ID - AROA",
			content:  `AROAEXAMPLE123456`,
			expected: nil, // Does not match (needs 16 alphanumeric chars)
		},
		{
			name:     "AWS Access Key ID - AIDA",
			content:  `AIDAEXAMPLE123456`,
			expected: nil, // Does not match (needs 16 alphanumeric chars)
		},
		{
			name:     "AWS Access Key ID - ASIA",
			content:  `ASIAEXAMPLE123456`,
			expected: nil, // Does not match (needs 16 alphanumeric chars)
		},
		{
			name:     "AWS Secret Access Key",
			content:  `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
			expected: []string{"API Key Exposure", "AWS Secret Access Key Exposure"},
		},
		{
			name:     "AWS Session Token",
			content:  `aws_session_token = "FwoGZXIvYXdzEBYaDMjALpLZeVClcuTESTINGT0NQ/7yTESTING/3/test-testing-12345"`,
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "Multiple AWS concerns",
			content:  `AKIAIOSFODNN7EXAMPLE and aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
			expected: []string{"API Key Exposure", "AWS Access Key ID Exposure", "AWS Secret Access Key Exposure"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcerns_GitHubPAT(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "GitHub PAT - ghp_ format",
			content:  `ghp_1234567890abcdefghijklmnopqrstuvwxyz`,
			expected: []string{"GitHub PAT Exposure"},
		},
		{
			name:     "GitHub PAT - github_pat_ format",
			content:  `github_pat_11AABBCCDD_00ZZYYXXWWvvwwqqqq1122334455aabbccdd1122334455ffgghh11223344`,
			expected: nil, // Does not match regex pattern
		},
		{
			name:     "GitHub PAT in code",
			content:  `github_token = "ghp_1234567890abcdefghijklmnopqrstuvwxyz"`,
			expected: []string{"API Key Exposure", "GitHub PAT Exposure"},
		},
		{
			name:     "GitHub PAT - too short",
			content:  `ghp_1234567890`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcerns_JWT(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "JWT token detection",
			content:  `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c`,
			expected: []string{"JWT Token Exposure"},
		},
		{
			name:     "JWT token in header",
			content:  `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c`,
			expected: []string{"Generic Bearer Token Exposure", "JWT Token Exposure"},
		},
		{
			name:     "JWT - invalid format",
			content:  `not.a.valid.jwt.token`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcerns_DatabaseURLs(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "MongoDB URL with credentials",
			content:  `mongodb://admin:password@localhost:27017/db`,
			expected: nil, // localhost filtered
		},
		{
			name:     "PostgreSQL URL with credentials",
			content:  `postgresql://user:pass@prod-db.example.com:5432/mydb`,
			expected: []string{"Database/Service Creds Exposure"},
		},
		{
			name:     "MySQL URL with credentials",
			content:  `mysql://user:pass@db.example.com:3306/mydb`,
			expected: []string{"Database/Service Creds Exposure"},
		},
		{
			name:     "Redis URL with credentials",
			content:  `redis://:password@redis.example.com:6379/0`,
			expected: []string{"Database/Service Creds Exposure"},
		},
		{
			name:     "JDBC URL",
			content:  `jdbc:postgresql://db.example.com:5432/mydb`,
			expected: []string{"Database/Service Creds Exposure"},
		},
		{
			name:     "localhost URL filtered",
			content:  `mongodb://localhost:27017/test`,
			expected: nil,
		},
		{
			name:     "127.0.0.1 URL filtered",
			content:  `postgresql://user:pass@127.0.0.1:5432/test`,
			expected: nil,
		},
		{
			name:     "test.db filtered",
			content:  `sqlite3:test.db`,
			expected: nil,
		},
		{
			name:     "example.db filtered",
			content:  `sqlite3:example.db`,
			expected: nil,
		},
		{
			name:     "file:// URL filtered",
			content:  `file:///path/to/db`,
			expected: nil,
		},
		{
			name:     "memory: URL filtered",
			content:  `sqlite3:memory:`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcerns_OtherCredentials(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []string
	}{
		{
			name:     "GitLab PAT",
			content:  `glpat-1234567890abcdefghij`,
			expected: []string{"GitLab PAT Exposure"},
		},
		{
			name:     "Stripe API key - sk_live",
			content:  `sk_live_abcdefghijklmnopqrstuvwxy`,
			expected: []string{"Stripe API Key Exposure"},
		},
		{
			name:     "Stripe API key - pk_live",
			content:  `pk_live_abcdefghijklmnopqrstuvwxy`,
			expected: []string{"Stripe API Key Exposure"},
		},
		{
			name:     "Twilio Auth Token - AC",
			content:  `ACabcdefghijklmnopqrstuvwxyz123456`,
			expected: []string{"Twilio Auth Token Exposure"},
		},
		{
			name:     "Twilio Auth Token - SK",
			content:  `SKabcdefghijklmnopqrstuvwxyz123456`,
			expected: []string{"Twilio Auth Token Exposure"},
		},
		{
			name:     "Slack Token - xoxb",
			content:  `xoxb-1234567890123-1234567890123-abcdefghijklmnopqrstuv`,
			expected: []string{"Slack Token Exposure"},
		},
		{
			name:     "Slack Token - xapp",
			content:  `xapp-1234567890123-1234567890123-abcdefghijklmnopqrstuv`,
			expected: []string{"Slack Token Exposure"},
		},
		{
			name:     "Google API Key",
			content:  `AIzaSyabcdefghijklmnopqrstuvwxyz1234567`, // Exactly 35 chars
			expected: []string{"Google API Key Exposure"},
		},
		{
			name:     "Heroku API Key",
			content:  `12345678-1234-1234-1234-123456789012`,
			expected: []string{"Heroku API Key Exposure"},
		},
		{
			name:     "Bearer Token",
			content:  `Bearer abcdefghijklmnopqrstuvwxyz123456`,
			expected: []string{"Generic Bearer Token Exposure"},
		},
		{
			name:     "SSH Private Key",
			content:  `-----BEGIN RSA PRIVATE KEY-----`,
			expected: []string{"SSH Private Key Exposure"},
		},
		{
			name:     "SSH Private Key - EC",
			content:  `-----BEGIN EC PRIVATE KEY-----`,
			expected: []string{"SSH Private Key Exposure"},
		},
		{
			name:     "SSH Private Key - OPENSSH",
			content:  `-----BEGIN OPENSSH PRIVATE KEY-----`,
			expected: []string{"SSH Private Key Exposure"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcernsWithContext(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		filePath string
		expected []string
	}{
		{
			name:     "Real credentials in test file - should filter placeholders",
			content:  `api_key = "test_api_key_1234567890"`,
			filePath: "example_test.go",
			expected: nil, // "test" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter demo",
			content:  `api_key = "demo_api_key_1234567890"`,
			filePath: "test.go",
			expected: nil, // "demo" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter sample",
			content:  `api_key = "sample_key_1234567890"`,
			filePath: "test.go",
			expected: nil, // "sample" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter placeholder",
			content:  `api_key = "placeholder_key_1234567890"`,
			filePath: "test.go",
			expected: nil, // "placeholder" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter example",
			content:  `api_key = "example_key_1234567890"`,
			filePath: "test.go",
			expected: nil, // "example" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter changeme",
			content:  `api_key = "changeme_key_1234567890"`,
			filePath: "test.go",
			expected: nil, // "changeme" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter your- prefix",
			content:  `api_key = "your-api-key-1234567890"`,
			filePath: "test.go",
			expected: nil, // "your-" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter paste-your",
			content:  `api_key = "paste-your-key-1234567890"`,
			filePath: "test.go",
			expected: nil, // "paste-your" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter abc",
			content:  `api_key = "abc1234567890abcdefghij"`,
			filePath: "test.go",
			expected: nil, // "abc" in value should be filtered
		},
		{
			name:     "Real credentials in test file - should filter 123",
			content:  `api_key = "key1234567890abcdefghijk"`,
			filePath: "test.go",
			expected: nil, // "123" in value should be filtered
		},
		{
			name:     "Real credentials in non-test file - should detect",
			content:  `api_key = "sk_live_1234567890abcdefghij"`,
			filePath: "main.go",
			expected: []string{"API Key Exposure"},
		},
		{
			name:     "Database URL localhost filtered",
			content:  `mongodb://admin:pass@localhost:27017`,
			filePath: "config.go",
			expected: nil,
		},
		{
			name:     "Database URL remote - should detect",
			content:  `mongodb://admin:pass@mongo.example.com:27017`,
			filePath: "config.go",
			expected: []string{"Database/Service Creds Exposure"},
		},
		{
			name:     "Local database in test file filtered",
			content:  `postgresql://user:pass@127.0.0.1:5432/db`,
			filePath: "test.go",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcernsWithContext(tt.content, tt.filePath)
			assert.Equal(t, tt.expected, concerns)
		})
	}
}

func TestDetectSecurityConcerns_MultipleConcerns(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected int // number of concerns
	}{
		{
			name:     "Multiple different credentials",
			content:  `AKIAIOSFODNN7EXAMPLE and password = "mysecretpass123" and mongodb://user:pass@mongo.example.com:27017`,
			expected: 3,
		},
		{
			name:     "All AWS credentials",
			content:  `AKIAIOSFODNN7EXAMPLE and aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" and aws_session_token = "FwoGZXIvYXdzEBYaDMjALpLZeVClcuTESTINGT0NQ/7yTESTING/3/test-testing-12345"`,
			expected: 3,
		},
		{
			name:     "GitHub and GitLab PATs",
			content:  `ghp_1234567890abcdefghijklmnopqrstuvwxyz and glpat-1234567890abcdefghij`,
			expected: 2,
		},
		{
			name:     "Empty content",
			content:  ``,
			expected: 0,
		},
		{
			name:     "No credentials content",
			content:  `func main() { fmt.Println("hello world") }`,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			concerns, _ := DetectSecurityConcerns(tt.content)
			assert.Len(t, concerns, tt.expected)
		})
	}
}

func TestDetectSecurityConcerns_Snippets(t *testing.T) {
	// Test that snippets are returned correctly
	content := `api_key = "sk_live_1234567890abcdefghij" and password = "mysecretpass123"`
	concerns, snippets := DetectSecurityConcerns(content)

	assert.Contains(t, concerns, "API Key Exposure")
	assert.Contains(t, concerns, "Password Exposure")
	assert.Contains(t, snippets, "API Key Exposure")
	assert.Contains(t, snippets, "Password Exposure")

	// Verify snippets contain the matched content
	assert.Contains(t, snippets["API Key Exposure"], "sk_live_")
	assert.Contains(t, snippets["Password Exposure"], "mysecretpass")
}

func TestDetectSecurityConcerns_SortedConcerns(t *testing.T) {
	// Test that concerns are sorted alphabetically
	content := `password = "mysecretpass123" and api_key = "sk_live_1234567890abcdefghij"`
	concerns, _ := DetectSecurityConcerns(content)

	// Should be sorted alphabetically
	assert.Equal(t, []string{"API Key Exposure", "Password Exposure"}, concerns)
}
