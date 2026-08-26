//go:build !js

package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/sprout-foundry/sprout/pkg/txn"
)

// ETH-2 "transactional escalation": the CLI mirror of the daemon's /api/txn/*
// surface, so the platform (or a human) can drive the same three-phase
// transaction by shelling into the container instead of hitting the daemon.
//
// stdout is EXACTLY the pinned JSON of the corresponding contract shape — a
// single object per invocation — so it can be piped straight into a parser;
// human-readable logging goes to stderr only.
//
// Exit codes: 0 for every reportable state (a partial apply, a failed
// command, a not-a-repo directory); 1 only for a usage or IO error, in which
// case {"error":"..."} is printed to stdout instead.

var (
	txnStatusRepoDir string
	txnPushRepoDir   string
	txnPushIn        string
	txnPullRepoDir   string
	txnPullOut       string
)

var txnStatusCmd = &cobra.Command{
	Use:   "txn-status",
	Short: "Report workspace tree state (dirty/untracked/deleted files) as JSON",
	Long: `Print the ETH-2 transaction status JSON for a repository.

This is the preflight half of a transaction: which tracked files are dirty,
which are untracked, and which are deleted, plus the branch and a timestamp.
stdout is EXACTLY the contract object; logging goes to stderr.

Strictly read-only — one "git status" and one branch probe, no fetch and no
write. Exit 0 for every reportable state (including "not a git repository").`,
	RunE: runTxnStatusCmd,
}

var txnPushCmd = &cobra.Command{
	Use:   "txn-push",
	Short: "Apply a delta manifest (files + deletes) to a directory from a file or stdin",
	Long: `Apply an ETH-2 delta manifest to a directory and print the apply result.

The manifest is the pinned push contract:
  {"base":{...},"files":[{"path":..,"content_base64":..,"size":..,"mode":..}],
   "deletes":[..],"truncated":false,"skipped":[]}

Read it from --in <path> or "-" for stdin (JSON). stdout is the apply result:
  {"applied":N,"deleted":N,"skipped":[..],"status":"ok"|"partial"}

Every entry is validated before it touches disk: unsafe paths, bad base64 and
over-cap entries are skipped with a reason rather than failing the request.
Exit 0 for every reportable state, including "partial".`,
	RunE: runTxnPushCmd,
}

var txnPullCmd = &cobra.Command{
	Use:   "txn-pull",
	Short: "Build a delta manifest from the working tree and write it to a file or stdout",
	Long: `Print the ETH-2 pull manifest computed from a repository's working tree.

Every dirty tracked file and every untracked file is base64-encoded into
"files"; deleted tracked files are listed in "deletes". Caps are honored by
omission — over-cap entries land in "skipped" and set "truncated".

Never touches the working tree: no add, no stash, no reset.
Write it to --out <path> or "-" for stdout (the default). Exit 0 for every
reportable state, including "partial"/truncated manifests.`,
	RunE: runTxnPullCmd,
}

func runTxnStatusCmd(cmd *cobra.Command, args []string) error {
	repoDir, err := resolveTxnDir(cmd, txnStatusRepoDir)
	if err != nil {
		return err
	}
	status, err := txn.BuildStatus(context.Background(), repoDir)
	if err != nil {
		return writeTxnCmdError(cmd, err)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
}

func runTxnPushCmd(cmd *cobra.Command, args []string) error {
	repoDir, err := resolveTxnDir(cmd, txnPushRepoDir)
	if err != nil {
		return err
	}
	var manifest txn.DeltaManifest
	if err := decodeTxnManifestFrom(cmd, txnPushIn, &manifest); err != nil {
		return writeTxnCmdError(cmd, err)
	}
	result, err := txn.ApplyDelta(context.Background(), repoDir, manifest)
	if err != nil {
		return writeTxnCmdError(cmd, err)
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
}

func runTxnPullCmd(cmd *cobra.Command, args []string) error {
	repoDir, err := resolveTxnDir(cmd, txnPullRepoDir)
	if err != nil {
		return err
	}
	manifest, err := txn.BuildPull(context.Background(), repoDir)
	if err != nil {
		return writeTxnCmdError(cmd, err)
	}
	out, closeOut, err := openTxnOutput(cmd, txnPullOut)
	if err != nil {
		return writeTxnCmdError(cmd, err)
	}
	defer closeOut()
	return json.NewEncoder(out).Encode(manifest)
}

// resolveTxnDir resolves --dir, defaulting to the process working directory.
func resolveTxnDir(cmd *cobra.Command, dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", writeTxnCmdError(cmd, fmt.Errorf("failed to resolve working directory: %w", err))
	}
	return cwd, nil
}

// decodeTxnManifestFrom reads the manifest from a path, or stdin for "-" and
// the empty default. Unlike the daemon route there is no request cap to
// enforce — the platform already trusts the pipe it is writing into.
func decodeTxnManifestFrom(cmd *cobra.Command, in string, dst *txn.DeltaManifest) error {
	var reader io.Reader
	switch in {
	case "", "-":
		reader = cmd.InOrStdin()
	default:
		file, err := os.Open(in)
		if err != nil {
			return fmt.Errorf("txn-push: open %s: %w", in, err)
		}
		defer file.Close()
		reader = file
	}
	if err := json.NewDecoder(reader).Decode(dst); err != nil {
		return fmt.Errorf("txn-push: manifest is not valid JSON: %w", err)
	}
	return nil
}

// openTxnOutput resolves --out: a file, or stdout for "-" and the default.
func openTxnOutput(cmd *cobra.Command, out string) (io.Writer, func(), error) {
	switch out {
	case "", "-":
		return cmd.OutOrStdout(), func() {}, nil
	default:
		file, err := os.Create(out)
		if err != nil {
			return nil, nil, fmt.Errorf("txn-pull: create %s: %w", out, err)
		}
		return file, func() { _ = file.Close() }, nil
	}
}

// writeTxnCmdError renders the failure shape. stdout stays machine-readable
// ({"error":"..."}), human context goes to stderr, and the error is marked
// reported so cmd.Execute() still exits 1 without re-printing it.
func writeTxnCmdError(cmd *cobra.Command, err error) error {
	fmt.Fprintf(cmd.ErrOrStderr(), "sprout txn: %v\n", err)
	_ = json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
	return markReported(err)
}

func init() {
	txnStatusCmd.Flags().StringVar(&txnStatusRepoDir, "dir", "", "Directory to inspect (defaults to the current working directory)")
	txnPushCmd.Flags().StringVar(&txnPushRepoDir, "dir", "", "Directory to apply the manifest to (defaults to the current working directory)")
	txnPushCmd.Flags().StringVar(&txnPushIn, "in", "-", "Manifest input path, or - for stdin")
	txnPullCmd.Flags().StringVar(&txnPullRepoDir, "dir", "", "Directory to read the working tree from (defaults to the current working directory)")
	txnPullCmd.Flags().StringVar(&txnPullOut, "out", "-", "Manifest output path, or - for stdout")
}
