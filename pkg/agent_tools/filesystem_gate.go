package tools

// This file previously contained the FilesystemGate interface and
// withFilesystemApproval retry wrapper. SP-127 M4 removed them:
// - FilesystemGate and its implementations were removed (the PrecheckFileAccess
//   path is now the sole entry point)
// - withFilesystemApproval and related helpers were removed (off-workspace
//   paths now fail with raw filesystem errors)
//
// Left for reference only:
// - resolveCanonicalForDisplay was used by withFilesystemApproval to show
//   symlink targets in approval dialogs. Since the gate is gone, so is it.
