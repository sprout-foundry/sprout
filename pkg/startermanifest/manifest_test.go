package startermanifest

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureValid is the canonical on-disk manifest document: a fully-populated,
// valid .sprout/starter.json. It doubles as the fixture the "fully-populated
// manifest passes" case validates.
const fixtureValid = `{
  "starter": {"id": "web-app", "version": "1.2.0"},
  "build": "npm run build",
  "test": "npm test",
  "dev": "npm run dev",
  "preview": "npx serve dist",
  "format": "npm run format",
  "lint": "npm run lint",
  "dev_port": 5173,
  "routes": ["/", "/login", "/dashboard"],
  "build_output": "dist"
}`

// fixtureMinimal is the leanest valid manifest: just the starter identity.
// Every command, the port, the routes, and the build output are optional
// (any project can add the file by hand).
const fixtureMinimal = `{"starter": {"id": "static-site", "version": "0.1.0"}}`

// TestValidateJSON is the table test over JSON fixtures for the schema:
// a fully-populated manifest and the minimal manifest pass, and every
// invalid case fails with a clear message.
func TestValidateJSON(t *testing.T) {
	cases := []struct {
		name       string
		jsonStr    string
		wantValid  bool
		wantSubstr string // when set, the error must contain this substring
	}{
		{
			name:      "fully-populated valid manifest passes",
			jsonStr:   fixtureValid,
			wantValid: true,
		},
		{
			name:      "minimal manifest (starter identity only) passes",
			jsonStr:   fixtureMinimal,
			wantValid: true,
		},
		{
			name:      "absent build_output and commands is lenient",
			jsonStr:   `{"starter": {"id": "static-site", "version": "0.1.0"}, "build": "make site"}`,
			wantValid: true,
		},
		{
			name:      "dev_port of 0 means no fixed port and passes",
			jsonStr:   `{"starter": {"id": "web-app", "version": "1.0.0"}, "dev": "npm run dev", "dev_port": 0}`,
			wantValid: true,
		},
		{
			name:       "missing starter id fails",
			jsonStr:    strings.Replace(fixtureValid, `"id": "web-app"`, `"id": ""`, 1),
			wantValid:  false,
			wantSubstr: "starter.id is required",
		},
		{
			name:       "missing starter version fails",
			jsonStr:    strings.Replace(fixtureValid, `"version": "1.2.0"`, `"version": ""`, 1),
			wantValid:  false,
			wantSubstr: "starter.version is required",
		},
		{
			name:       "whitespace-only build command fails",
			jsonStr:    strings.Replace(fixtureValid, `"build": "npm run build"`, `"build": "   "`, 1),
			wantValid:  false,
			wantSubstr: "build: command must not be whitespace-only",
		},
		{
			name:       "whitespace-only dev command fails",
			jsonStr:    strings.Replace(fixtureValid, `"dev": "npm run dev"`, `"dev": "  "`, 1),
			wantValid:  false,
			wantSubstr: "dev: command must not be whitespace-only",
		},
		{
			name:       "whitespace-only preview command fails",
			jsonStr:    strings.Replace(fixtureValid, `"preview": "npx serve dist"`, `"preview": "   "`, 1),
			wantValid:  false,
			wantSubstr: "preview: command must not be whitespace-only",
		},
		{
			name:       "whitespace-only format command fails",
			jsonStr:    strings.Replace(fixtureValid, `"format": "npm run format"`, `"format": "   "`, 1),
			wantValid:  false,
			wantSubstr: "format: command must not be whitespace-only",
		},
		{
			name:       "whitespace-only lint command fails",
			jsonStr:    strings.Replace(fixtureValid, `"lint": "npm run lint"`, `"lint": "   "`, 1),
			wantValid:  false,
			wantSubstr: "lint: command must not be whitespace-only",
		},
		{
			name:       "out-of-range dev_port fails",
			jsonStr:    strings.Replace(fixtureValid, `"dev_port": 5173,`, `"dev_port": 70000,`, 1),
			wantValid:  false,
			wantSubstr: "dev_port must be a valid port",
		},
		{
			name:       "negative dev_port fails",
			jsonStr:    strings.Replace(fixtureValid, `"dev_port": 5173,`, `"dev_port": -1,`, 1),
			wantValid:  false,
			wantSubstr: "dev_port must be a valid port",
		},
		{
			name:       "empty route entry fails, naming the index",
			jsonStr:    strings.Replace(fixtureValid, `"/"`, `""`, 1),
			wantValid:  false,
			wantSubstr: "routes[0] must not be empty",
		},
		{
			name:       "whitespace-only build_output fails",
			jsonStr:    strings.Replace(fixtureValid, `"dist"`, `"   "`, 1),
			wantValid:  false,
			wantSubstr: "build_output: must not be whitespace-only",
		},
		{
			name:       "malformed JSON is a decode error",
			jsonStr:    `{"starter": {`,
			wantValid:  false,
			wantSubstr: "invalid starter JSON",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := ValidateJSON([]byte(tc.jsonStr))
			if tc.wantValid {
				require.NoError(t, err, "expected a valid manifest")
				require.NotNil(t, m)
				return
			}
			require.Error(t, err, "expected validation to fail")
			if tc.wantSubstr == "invalid starter JSON" {
				// A decode error, not a *ValidationError: the manifest bytes
				// are not JSON at all, so nothing is returned.
				assert.Nil(t, m, "a decode failure must not return a manifest")
				assert.Contains(t, err.Error(), tc.wantSubstr, "got: %v", err)
				return
			}
			require.NotNil(t, m, "the decoded manifest should still be returned")
			var vErr *ValidationError
			require.ErrorAs(t, err, &vErr, "expected a *ValidationError")
			assert.NotEmpty(t, vErr.Problems, "expected at least one problem")
			if tc.wantSubstr != "" {
				assert.Contains(t, err.Error(), tc.wantSubstr, "got: %v", err)
			}
		})
	}
}

// baseManifest returns a fully-populated, valid manifest so the struct-level
// table below can mutate one field in isolation.
func baseManifest() *StarterManifest {
	m := New("web-app", "1.2.0")
	m.Build = "npm run build"
	m.Test = "npm test"
	m.Dev = "npm run dev"
	m.Preview = "npx serve dist"
	m.Format = "npm run format"
	m.Lint = "npm run lint"
	m.DevPort = 5173
	m.Routes = []string{"/", "/login", "/dashboard"}
	m.BuildOutput = "dist"
	return m
}

// TestValidateTable is the struct-level table test covering the validator
// rules in isolation.
func TestValidateTable(t *testing.T) {
	mutate := func(fn func(m *StarterManifest)) *StarterManifest {
		m := baseManifest()
		fn(m)
		return m
	}

	cases := []struct {
		name       string
		m          *StarterManifest
		wantValid  bool
		wantSubstr string
	}{
		{
			name:      "valid manifest passes",
			m:         baseManifest(),
			wantValid: true,
		},
		{
			name:       "nil manifest fails",
			m:          nil,
			wantValid:  false,
			wantSubstr: "manifest is nil",
		},
		{
			name:       "blank starter id fails",
			m:          mutate(func(m *StarterManifest) { m.Starter.ID = "   " }),
			wantValid:  false,
			wantSubstr: "starter.id is required",
		},
		{
			name:       "blank starter version fails",
			m:          mutate(func(m *StarterManifest) { m.Starter.Version = "" }),
			wantValid:  false,
			wantSubstr: "starter.version is required",
		},
		{
			name:       "whitespace-only test command fails",
			m:          mutate(func(m *StarterManifest) { m.Test = "  " }),
			wantValid:  false,
			wantSubstr: "test: command must not be whitespace-only",
		},
		{
			name:       "whitespace-only format command fails",
			m:          mutate(func(m *StarterManifest) { m.Format = "  " }),
			wantValid:  false,
			wantSubstr: "format: command must not be whitespace-only",
		},
		{
			name:       "whitespace-only lint command fails",
			m:          mutate(func(m *StarterManifest) { m.Lint = "  " }),
			wantValid:  false,
			wantSubstr: "lint: command must not be whitespace-only",
		},
		{
			name:      "dev_port of 0 (no fixed port) passes",
			m:         mutate(func(m *StarterManifest) { m.DevPort = 0 }),
			wantValid: true,
		},
		{
			name:       "dev_port above 65535 fails",
			m:          mutate(func(m *StarterManifest) { m.DevPort = 70000 }),
			wantValid:  false,
			wantSubstr: "dev_port must be a valid port",
		},
		{
			name:       "negative dev_port fails",
			m:          mutate(func(m *StarterManifest) { m.DevPort = -1 }),
			wantValid:  false,
			wantSubstr: "dev_port must be a valid port",
		},
		{
			name:       "blank route entry fails, naming the index",
			m:          mutate(func(m *StarterManifest) { m.Routes[1] = "  " }),
			wantValid:  false,
			wantSubstr: "routes[1] must not be empty",
		},
		{
			name:       "whitespace-only build_output fails",
			m:          mutate(func(m *StarterManifest) { m.BuildOutput = "   " }),
			wantValid:  false,
			wantSubstr: "build_output: must not be whitespace-only",
		},
		{
			name:      "missing commands is lenient",
			m:         New("static-site", "0.1.0"),
			wantValid: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.m)
			if tc.wantValid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			if tc.wantSubstr != "" {
				assert.Contains(t, err.Error(), tc.wantSubstr, "got: %v", err)
			}
		})
	}
}

// TestValidateReportsAllProblems confirms the validator surfaces every
// problem at once (not just the first), which is what makes the messages
// actionable in a single edit pass.
func TestValidateReportsAllProblems(t *testing.T) {
	m := baseManifest()
	m.Starter.ID = "   "   // starter.id required
	m.Starter.Version = "" // starter.version required
	m.Build = "  "         // whitespace-only command
	m.DevPort = 70000      // invalid port
	m.Routes[1] = ""       // blank route

	err := Validate(m)
	require.Error(t, err)
	var vErr *ValidationError
	require.ErrorAs(t, err, &vErr)
	assert.GreaterOrEqual(t, len(vErr.Problems), 5,
		"expected every problem to be reported, got: %v", vErr.Problems)
}

// TestJSONFieldNamesMatchSpec pins the on-disk contract: the top-level JSON
// keys must be exactly the spec's field list, and the nested starter item
// must use the documented field names.
func TestJSONFieldNamesMatchSpec(t *testing.T) {
	b, err := json.Marshal(baseManifest())
	require.NoError(t, err)

	var m map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &m))

	wantTopLevel := []string{
		"starter", "build", "test", "dev", "preview", "format", "lint", "dev_port", "routes", "build_output",
	}
	for _, k := range wantTopLevel {
		assert.Contains(t, m, k, "missing top-level JSON key %q", k)
	}
	assert.Equal(t, len(wantTopLevel), len(m), "unexpected top-level JSON keys: %v", keys(m))

	var starter struct {
		ID      string `json:"id"`
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(m["starter"], &starter))
	assert.Equal(t, "web-app", starter.ID)
	assert.Equal(t, "1.2.0", starter.Version)
}

// TestMinimalWireFormat pins the omitempty rules: a minimal manifest
// (starter identity only) must serialize to exactly its starter field, so a
// lean hand-authored file stays compact on disk.
func TestMinimalWireFormat(t *testing.T) {
	b, err := json.Marshal(New("web-app", "1.0.0"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"starter": {"id": "web-app", "version": "1.0.0"}}`, string(b))
}

// TestRoundTrip confirms a manifest survives a JSON marshal/unmarshal cycle
// unchanged (the contract that makes .sprout/starter.json a stable file).
func TestRoundTrip(t *testing.T) {
	m := baseManifest()
	b, err := json.Marshal(m)
	require.NoError(t, err)

	var decoded StarterManifest
	require.NoError(t, json.Unmarshal(b, &decoded))
	require.NoError(t, Validate(&decoded))
	assert.Equal(t, *m, decoded, "round-tripped manifest should be identical")
}

// TestNewInitializesEmptyManifest checks New sets the starter identity and
// initializes Routes to an empty (non-nil) slice.
func TestNewInitializesEmptyManifest(t *testing.T) {
	m := New("web-app", "1.0.0")

	assert.Equal(t, "web-app", m.Starter.ID)
	assert.Equal(t, "1.0.0", m.Starter.Version)
	assert.Empty(t, m.Build)
	assert.Empty(t, m.Test)
	assert.Empty(t, m.Dev)
	assert.Empty(t, m.Preview)
	assert.Zero(t, m.DevPort)
	assert.Empty(t, m.BuildOutput)
	assert.NotNil(t, m.Routes)
	assert.Empty(t, m.Routes)
	require.NoError(t, Validate(m), "a freshly constructed manifest is valid")
}

func keys(m map[string]json.RawMessage) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
