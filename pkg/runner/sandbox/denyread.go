package sandbox

import "path/filepath"

// credentialPaths are relative to home and denied on every OS: listing a
// path that does not exist on this OS costs nothing and keeps the set in one
// place. ~/.npmrc is deliberately absent: npm reads registry configuration
// from it, and a project's install cannot work without it.
var credentialPaths = []string{
	".ssh",
	".aws",
	".azure",
	".kube",
	".gnupg",
	".password-store",
	".netrc",
	".docker/config.json",
	".config/gh",
	".config/gcloud",
	".config/sprout/credentials",
	"Library/Keychains",
	"Library/Cookies",
	"Library/Messages",
	"Library/Mail",
	"Library/Safari",
	"Library/Containers/com.apple.Safari",
	"Library/Application Support/Google/Chrome",
	"Library/Application Support/Firefox",
	".local/share/keyrings",
	".mozilla",
	".config/google-chrome",
	".config/chromium",
}

func defaultDenyRead(home string) []string {
	if home == "" {
		return nil
	}
	out := make([]string, 0, len(credentialPaths))
	for _, rel := range credentialPaths {
		out = append(out, filepath.Join(home, filepath.FromSlash(rel)))
	}
	return out
}
