package starters

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureVersion is the version the fixture tree declares; assertions
// here must agree with pkg/starters/data/fixture/starter.json.
const fixtureVersion = "0.1.0"

// TestList asserts the catalogue: every embedded starter is listed with a
// non-empty version, the result is sorted by id, and the fixture (the
// test-only starter that proves the mechanism) is present. It does not
// pin the total count, so product starters added in later items (153.4+)
// do not break the test.
func TestList(t *testing.T) {
	got, err := List()
	require.NoError(t, err)
	assert.NotEmpty(t, got, "the embedded catalogue must not be empty")

	for i := 1; i < len(got); i++ {
		assert.Less(t, got[i-1].ID, got[i].ID, "List must be sorted by id")
	}
	for _, s := range got {
		assert.NotEmpty(t, s.ID)
		assert.NotEmpty(t, s.Version, "starter %q must declare a version", s.ID)
	}

	byID := make(map[string]Starter, len(got))
	for _, s := range got {
		byID[s.ID] = s
	}
	f, ok := byID["fixture"]
	require.True(t, ok, "the fixture starter must be embedded")
	assert.Equal(t, fixtureVersion, f.Version)
}

func TestVersion(t *testing.T) {
	v, err := Version("fixture")
	require.NoError(t, err)
	assert.Equal(t, fixtureVersion, v)

	_, err = Version("no-such-starter")
	assert.ErrorIs(t, err, ErrUnknownStarter)

	_, err = Version("")
	assert.ErrorIs(t, err, ErrInvalidStarterID)

	_, err = Version("../fixture")
	assert.ErrorIs(t, err, ErrInvalidStarterID)
}

// TestManifest asserts the embedded descriptor round-trips through the
// startermanifest validator with exactly the values the fixture declares.
func TestManifest(t *testing.T) {
	m, err := Manifest("fixture")
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.Equal(t, "fixture", m.Starter.ID)
	assert.Equal(t, fixtureVersion, m.Starter.Version)
	assert.Equal(t, "npm run build", m.Build)
	assert.Equal(t, "npm test", m.Test)
	assert.Equal(t, "npm run dev", m.Dev)
	assert.Empty(t, m.Preview, "the fixture omits the optional preview command")
	assert.Equal(t, 3000, m.DevPort)
	assert.Equal(t, []string{"/"}, m.Routes)
	assert.Equal(t, "dist", m.BuildOutput)
}

// TestManifestErrors table-checks the error sentinels: unknown ids and
// malformed ids (path segments that could escape the embedded data tree)
// each fail with a distinguishable error before anything is touched.
func TestManifestErrors(t *testing.T) {
	cases := []struct {
		name  string
		id    string
		isErr error
	}{
		{name: "unknown", id: "no-such-starter", isErr: ErrUnknownStarter},
		{name: "empty", id: "", isErr: ErrInvalidStarterID},
		{name: "dot", id: ".", isErr: ErrInvalidStarterID},
		{name: "dotdot", id: "..", isErr: ErrInvalidStarterID},
		{name: "slash", id: "a/b", isErr: ErrInvalidStarterID},
		{name: "backslash", id: `a\b`, isErr: ErrInvalidStarterID},
		{name: "absolute", id: "/abs", isErr: ErrInvalidStarterID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Manifest(tc.id)
			assert.ErrorIs(t, err, tc.isErr)
			_, err = Version(tc.id)
			assert.ErrorIs(t, err, tc.isErr)
		})
	}
}

// TestFileCount pins the catalogue's tree-size fact: the fixture tree
// carries three project-content files (README.md, index.html,
// src/main.js); the descriptor is never a project file, so it is not
// counted. Error sentinels match Manifest's.
func TestFileCount(t *testing.T) {
	n, err := FileCount("fixture")
	require.NoError(t, err)
	assert.Equal(t, 3, n, "the fixture tree has 3 project-content files (the descriptor becomes .sprout/starter.json, not a copied file)")

	_, err = FileCount("no-such-starter")
	assert.ErrorIs(t, err, ErrUnknownStarter)

	_, err = FileCount("../fixture")
	assert.ErrorIs(t, err, ErrInvalidStarterID)
}

// TestEmbeddedTreeIntegrity is the discovery test over every embedded
// tree: each data/ directory must carry a valid descriptor whose starter
// id matches the directory name, with a non-empty version. A directory
// that fails this is a repo bug the build-time test catches (the same
// "discovery test" convention as pkg/skills).
func TestEmbeddedTreeIntegrity(t *testing.T) {
	entries, err := fs.ReadDir(dataFS, dataRoot)
	require.NoError(t, err)
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ids = append(ids, e.Name())
	}
	require.NotEmpty(t, ids, "the embedded data tree must contain at least one starter")

	for _, id := range ids {
		m, err := manifestFor(id)
		require.NoErrorf(t, err, "starter %q must carry a valid descriptor", id)
		assert.Equal(t, id, m.Starter.ID, "directory name and descriptor id must agree")
		assert.NotEmpty(t, m.Starter.Version, "starter %q must declare a version", id)
	}
}
