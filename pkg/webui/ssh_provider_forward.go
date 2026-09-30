//go:build !js

package webui

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// sshForwardEnd ends the provider block the launch script reads from stdin.
const sshForwardEnd = "SPROUT_FORWARD_END"

var sshForwardNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// sshForwardedProvider is this machine's active provider, handed to an SSH
// host's daemon so every workspace works with the same model and key. The key
// only ever lives in the remote daemon's environment: it travels over the ssh
// connection's stdin (never the command line) and is never written to disk.
type sshForwardedProvider struct {
	Provider string
	Model    string
	// Env holds the variables to set on the remote daemon (provider, model
	// and, for providers that need one, the API key).
	Env map[string]string
	// Definition is a custom provider's definition file. It names the key's
	// env var but holds no secret; the remote needs it to know the provider.
	Definition []byte
}

// activeProviderModel is the provider and model a client is working with:
// its agent's, or the configured ones before it has an agent.
func (ws *ReactWebServer) activeProviderModel(clientID string) (string, string) {
	if ag, err := ws.getClientAgent(clientID); err == nil && ag != nil {
		return ag.GetProvider(), ag.GetModel()
	}
	cm, err := configuration.NewManager()
	if err != nil {
		return "", ""
	}
	providerType, model, err := configuration.ResolveProviderModel(cm.GetConfig(), "", "")
	if err != nil {
		return "", ""
	}
	return string(providerType), model
}

// forwardedProviderFor captures a provider and model the user is working
// with here. Returns nil when there is nothing to forward.
func forwardedProviderFor(provider, model string) (*sshForwardedProvider, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" || !sshForwardNamePattern.MatchString(provider) {
		return nil, nil
	}
	fwd := &sshForwardedProvider{
		Provider: provider,
		Model:    strings.TrimSpace(model),
		Env:      map[string]string{"SPROUT_PROVIDER": provider},
	}
	if fwd.Model != "" {
		fwd.Env["SPROUT_MODEL"] = fwd.Model
	}
	cred, err := credentials.ResolveProvider(provider)
	if err != nil {
		return nil, fmt.Errorf("resolve %s credential: %w", provider, err)
	}
	if cred.Value != "" && sshForwardNamePattern.MatchString(cred.EnvVar) {
		fwd.Env[cred.EnvVar] = cred.Value
	}
	if customs, err := configuration.LoadCustomProviders(); err == nil {
		if _, ok := customs[provider]; ok {
			if path, err := configuration.GetCustomProviderPath(provider); err == nil {
				if data, err := os.ReadFile(path); err == nil {
					fwd.Definition = data
				}
			}
		}
	}
	return fwd, nil
}

// fingerprint identifies the forwarded settings without revealing them, so
// the remote can tell whether its running daemon already has them.
func (f *sshForwardedProvider) fingerprint() string {
	h := sha256.New()
	keys := make([]string, 0, len(f.Env))
	for k := range f.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "%s=%s\x00", k, f.Env[k])
	}
	h.Write(f.Definition)
	return hex.EncodeToString(h.Sum(nil))
}

// stdinPayload is what the launch script reads before anything else: one
// record per line, values base64-encoded so no quoting survives into a shell.
func (f *sshForwardedProvider) stdinPayload() string {
	var b strings.Builder
	keys := make([]string, 0, len(f.Env))
	for k := range f.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "ENV %s %s\n", k, base64.StdEncoding.EncodeToString([]byte(f.Env[k])))
	}
	if len(f.Definition) > 0 {
		fmt.Fprintf(&b, "FILE %s %s\n", f.Provider, base64.StdEncoding.EncodeToString(f.Definition))
	}
	fmt.Fprintf(&b, "FP %s -\n", f.fingerprint())
	b.WriteString(sshForwardEnd + "\n")
	return b.String()
}

// sshForwardReadScript reads the provider block off stdin into $_FWD. It runs
// before the rc files are sourced: an interactive subshell there could
// otherwise consume stdin.
var sshForwardReadScript = []string{
	`_FWD=""`,
	`if [ "$FORWARD_PROVIDER" = "1" ]; then`,
	`  while IFS= read -r _l; do`,
	`    [ "$_l" = "` + sshForwardEnd + `" ] && break`,
	`    _FWD="$_FWD$_l`,
	`"`,
	`  done`,
	`fi`,
}

// sshForwardApplyScript exports the forwarded values after the rc files, so
// this machine's provider wins over anything the remote's shell sets, writes
// a custom provider's definition, and marks the daemon for a restart when it
// is running with different settings.
var sshForwardApplyScript = []string{
	`FORWARD_FP=""`,
	`_b64d() { base64 -d 2>/dev/null || base64 -D; }`,
	`_PDIR="${XDG_CONFIG_HOME:-$HOME/.config}/sprout/providers"`,
	`while IFS=' ' read -r _k _n _v; do`,
	`  case "$_k" in`,
	`    ENV) export "$_n=$(printf '%s' "$_v" | _b64d)" ;;`,
	`    FILE) mkdir -p "$_PDIR" && printf '%s' "$_v" | _b64d > "$_PDIR/$_n.json" ;;`,
	`    FP) FORWARD_FP="$_n" ;;`,
	`  esac`,
	`done <<SPROUT_FWD_EOF`,
	`$_FWD`,
	`SPROUT_FWD_EOF`,
	`unset _FWD _k _n _v`,
}
