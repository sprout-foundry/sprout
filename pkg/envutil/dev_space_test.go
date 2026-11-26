package envutil

import "testing"

func TestDevSpaceEnvPrefersTheCurrentNames(t *testing.T) {
	t.Setenv("WORKSPACE_ID", "old-id")
	t.Setenv("WORKSPACE_TOKEN", "old-token")
	if DevSpaceID() != "old-id" || DevSpaceToken() != "old-token" {
		t.Fatalf("earlier names are not read: %q %q", DevSpaceID(), DevSpaceToken())
	}
	t.Setenv("DEV_SPACE_ID", "id")
	t.Setenv("DEV_SPACE_TOKEN", "token")
	if DevSpaceID() != "id" || DevSpaceToken() != "token" {
		t.Fatalf("current names do not win: %q %q", DevSpaceID(), DevSpaceToken())
	}
}
