# Homebrew formula for sprout.
#
# Canonical source for the formula lives in this repo at Formula/sprout.rb.
# At release time, .github/workflows/release.yml uses the bumper script
# (scripts/update-homebrew-formula.sh) to substitute the version and
# per-platform SHA256s, then pushes the result to the consumer tap repo
# (sprout-foundry/homebrew-tap). Users `brew tap sprout-foundry/sprout`
# and `brew install sprout` — both run against the published copy, not
# this file.
#
# The placeholders below are intentionally invalid SHA256s; do NOT replace
# them by hand. Use scripts/update-homebrew-formula.sh after a release.

class Sprout < Formula
  desc "AI-powered code editing and assistance tool"
  homepage "https://github.com/sprout-foundry/sprout"
  version "0.0.0"
  license "MIT"

  # All four binary releases ship as tar.gz from the GitHub Release. The
  # per-OS / per-arch URL + sha256 pair tells Homebrew which artifact to
  # fetch for the current host. Homebrew picks at install time — no
  # multi-arch metadata file required.
  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/sprout-foundry/sprout/releases/download/v#{version}/sprout-darwin-arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"

      def install
        bin.install "sprout-darwin-arm64" => "sprout"
      end
    else
      url "https://github.com/sprout-foundry/sprout/releases/download/v#{version}/sprout-darwin-amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"

      def install
        bin.install "sprout-darwin-amd64" => "sprout"
      end
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/sprout-foundry/sprout/releases/download/v#{version}/sprout-linux-arm64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"

      def install
        bin.install "sprout-linux-arm64" => "sprout"
      end
    else
      url "https://github.com/sprout-foundry/sprout/releases/download/v#{version}/sprout-linux-amd64.tar.gz"
      sha256 "0000000000000000000000000000000000000000000000000000000000000000"

      def install
        bin.install "sprout-linux-amd64" => "sprout"
      end
    end
  end

  # Smoke test that runs as part of `brew test sprout`. Just confirms the
  # binary loads and reports a version string — doesn't make network
  # calls (Homebrew test isolation guidelines forbid them) and doesn't
  # touch ~/.config/sprout.
  test do
    output = shell_output("#{bin}/sprout version")
    assert_match(/sprout version/, output)
  end

  def caveats
    <<~EOS
      Sprout stores its configuration in ~/.config/sprout/ and session state
      in ~/.sprout/. To configure an LLM provider, run:

        sprout

      and follow the onboarding flow, or edit ~/.config/sprout/config.json
      directly.

      The Web UI runs on port 56000 by default — open http://localhost:56000
      once the daemon is running.
    EOS
  end
end
