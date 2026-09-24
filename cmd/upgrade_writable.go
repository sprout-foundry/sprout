//go:build !js

package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// moveFile copies src → dst then removes src. Used instead of os.Rename
// when the staging temp dir is on a different filesystem than the install
// dir (common: /tmp is tmpfs, /usr/local is the root fs).
func moveFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := in.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: close %s: %v\n", src, cerr)
		}
	}()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}

// probeWritableInstallDir confirms the current user can create (and
// remove) a file inside dir. It is the primitive that makes the
// "non-writable install dir" failure detectable *before* the upgrade
// downloads a 50MB tarball — the staging step in replaceBinary is the
// only place the dir is actually written, so probing the same dir is an
// exact check on Unix.
func probeWritableInstallDir(dir string) error {
	probe := filepath.Join(dir, ".sprout.write-probe")
	if err := os.WriteFile(probe, nil, 0600); err != nil {
		return err
	}
	return os.Remove(probe)
}

// requireWritableInstallDir fails fast (before any download) when the
// directory holding the already-resolved execPath can't be written to by
// the current user, naming the fix. On Windows the probe is advisory —
// the rename-over-running-image step still has to succeed — so a passing
// probe just means "don't get surprised later", while a failure is always
// real.
func requireWritableInstallDir(execPath string) error {
	dir := filepath.Dir(execPath)
	if err := probeWritableInstallDir(dir); err != nil {
		return fmt.Errorf("install dir %s is not writable: %w\n\n%s",
			dir, err, upgradeNotWritableHelp(execPath))
	}
	return nil
}

// upgradeNotWritableHelp renders the actionable guidance shown when the
// install dir can't be written to. The binary path is embedded so the
// message works no matter how the binary was installed.
func upgradeNotWritableHelp(execPath string) string {
	return fmt.Sprintf(`The current user can't replace the binary in place.
Pick one:
  sudo sprout upgrade     re-run with elevated privileges
  sudo chown <you> %s     take ownership, then retry
  SPROUT_INSTALL_DIR=~/.local/bin curl -fsSL \
      https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh
                          reinstall to a user-writable dir
`, execPath)
}
