package history

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestRecordChangeWithDetails_FullLifecycle tests the complete lifecycle of RecordChangeWithDetails.
func TestRecordChangeWithDetails_FullLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	oldChangesDir := changesDir
	oldRevisionsDir := revisionsDir
	defer func() {
		changesDir = oldChangesDir
		revisionsDir = oldRevisionsDir
	}()

	changesDir = filepath.Join(tmpDir, "changes")
	revisionsDir = filepath.Join(tmpDir, "revisions")

	// First record the base revision
	revisionID := "test-revision-lifecycle"
	instructions := "Add a new function"
	response := "Function added"
	conversation := []APIMessage{
		{Role: "user", Content: "Add a function"},
		{Role: "assistant", Content: "Adding function..."},
	}

	_, err := RecordBaseRevision(revisionID, instructions, response, conversation)
	if err != nil {
		t.Fatalf("RecordBaseRevision() error = %v", err)
	}

	// Now record a change with full details
	filename := "main.go"
	originalCode := "package main\n\nfunc main() {}"
	newCode := "package main\n\nfunc main() {\n\tAddFunction()\n}"
	description := "Call the new function"
	note := "User requested this change"
	originalPrompt := "Add a new function and call it"
	llmMessage := "Here's the updated code with the function call"
	editingModel := "test-model-v1"

	err = RecordChangeWithDetails(revisionID, filename, originalCode, newCode, description, note, originalPrompt, llmMessage, editingModel)
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error = %v", err)
	}

	// Verify the change was recorded
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges() error = %v", err)
	}

	if len(changes) != 1 {
		t.Errorf("GetAllChanges() got %d changes, want 1", len(changes))
		return
	}

	change := changes[0]
	if change.RequestHash != revisionID {
		t.Errorf("GetAllChanges() got RequestHash %q, want %q", change.RequestHash, revisionID)
	}
	if change.Filename != filename {
		t.Errorf("GetAllChanges() got Filename %q, want %q", change.Filename, filename)
	}
	if change.OriginalCode != originalCode {
		t.Errorf("GetAllChanges() got OriginalCode %q, want %q", change.OriginalCode, originalCode)
	}
	if change.NewCode != newCode {
		t.Errorf("GetAllChanges() got NewCode %q, want %q", change.NewCode, newCode)
	}
	if change.Description != description {
		t.Errorf("GetAllChanges() got Description %q, want %q", change.Description, description)
	}
	if change.Status != activeStatus {
		t.Errorf("GetAllChanges() got Status %q, want %q", change.Status, activeStatus)
	}
	if change.OriginalPrompt != originalPrompt {
		t.Errorf("GetAllChanges() got OriginalPrompt %q, want %q", change.OriginalPrompt, originalPrompt)
	}
	if change.LLMMessage != llmMessage {
		t.Errorf("GetAllChanges() got LLMMessage %q, want %q", change.LLMMessage, llmMessage)
	}
	if change.AgentModel != editingModel {
		t.Errorf("GetAllChanges() got AgentModel %q, want %q", change.AgentModel, editingModel)
	}
}

// TestRecordChangeWithDetails_Base64Encoding tests that file contents are base64 encoded.
func TestRecordChangeWithDetails_Base64Encoding(t *testing.T) {
	tmpDir := t.TempDir()
	oldChangesDir := changesDir
	oldRevisionsDir := revisionsDir
	defer func() {
		changesDir = oldChangesDir
		revisionsDir = oldRevisionsDir
	}()

	changesDir = filepath.Join(tmpDir, "changes")
	revisionsDir = filepath.Join(tmpDir, "revisions")

	// First record the base revision
	revisionID := "test-revision-base64"
	_, err := RecordBaseRevision(revisionID, "Make changes", "Done", nil)
	if err != nil {
		t.Fatalf("RecordBaseRevision() error = %v", err)
	}

	// Record a change with special characters that would be problematic in plain text
	filename := "test.go"
	originalCode := "package main\n\n// Special chars: <>&\"'\nfunc main() {}"
	newCode := "package main\n\n// Special chars: <>&\"'\nfunc main() {\n\t// Added\n}"
	description := "Add comment"
	note := "Test"

	err = RecordChangeWithDetails(revisionID, filename, originalCode, newCode, description, note, "", "", "")
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error = %v", err)
	}

	// Find the change directory
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		t.Fatalf("failed to read changes dir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 change directory, got %d", len(entries))
	}

	changeDir := entries[0].Name()
	safeFilename := filepath.Base(filename) // test.go -> test.go

	// Read the original file and verify it's base64 encoded
	originalPath := filepath.Join(changesDir, changeDir, safeFilename+".original")
	originalBytes, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatalf("failed to read original file: %v", err)
	}

	// Verify it's valid base64
	decoded, err := base64.StdEncoding.DecodeString(string(originalBytes))
	if err != nil {
		t.Fatalf("original file is not valid base64: %v", err)
	}

	if string(decoded) != originalCode {
		t.Errorf("decoded original code = %q, want %q", string(decoded), originalCode)
	}

	// Read the updated file and verify it's base64 encoded
	updatedPath := filepath.Join(changesDir, changeDir, safeFilename+".updated")
	updatedBytes, err := os.ReadFile(updatedPath)
	if err != nil {
		t.Fatalf("failed to read updated file: %v", err)
	}

	// Verify it's valid base64
	decoded, err = base64.StdEncoding.DecodeString(string(updatedBytes))
	if err != nil {
		t.Fatalf("updated file is not valid base64: %v", err)
	}

	if string(decoded) != newCode {
		t.Errorf("decoded new code = %q, want %q", string(decoded), newCode)
	}
}

// TestRecordChangeWithDetails_MetadataFields tests that all metadata fields are correctly stored.
func TestRecordChangeWithDetails_MetadataFields(t *testing.T) {
	tmpDir := t.TempDir()
	oldChangesDir := changesDir
	oldRevisionsDir := revisionsDir
	defer func() {
		changesDir = oldChangesDir
		revisionsDir = oldRevisionsDir
	}()

	changesDir = filepath.Join(tmpDir, "changes")
	revisionsDir = filepath.Join(tmpDir, "revisions")

	// First record the base revision
	revisionID := "test-revision-metadata"
	_, err := RecordBaseRevision(revisionID, "Make changes", "Done", nil)
	if err != nil {
		t.Fatalf("RecordBaseRevision() error = %v", err)
	}

	// Record a change with all details
	filename := "test.go"
	originalCode := "package main"
	newCode := "package main\n\nfunc main() {}"
	description := "Add main function"
	note := "Initial implementation"
	originalPrompt := "Create a main function"
	llmMessage := "Here's the code with main function"
	editingModel := "gpt-4-turbo"

	err = RecordChangeWithDetails(revisionID, filename, originalCode, newCode, description, note, originalPrompt, llmMessage, editingModel)
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error = %v", err)
	}

	// Find the change directory
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		t.Fatalf("failed to read changes dir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 change directory, got %d", len(entries))
	}

	changeDir := entries[0].Name()
	metadataPath := filepath.Join(changesDir, changeDir, metadataFile)

	// Read and parse metadata
	metadataBytes, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatalf("failed to read metadata: %v", err)
	}

	var metadata ChangeMetadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		t.Fatalf("failed to unmarshal metadata: %v", err)
	}

	// Verify all fields
	if metadata.Version != metadataVersion {
		t.Errorf("metadata.Version = %d, want %d", metadata.Version, metadataVersion)
	}
	if metadata.Filename != filename {
		t.Errorf("metadata.Filename = %q, want %q", metadata.Filename, filename)
	}
	if metadata.RequestHash != revisionID {
		t.Errorf("metadata.RequestHash = %q, want %q", metadata.RequestHash, revisionID)
	}
	if metadata.Status != activeStatus {
		t.Errorf("metadata.Status = %q, want %q", metadata.Status, activeStatus)
	}
	if metadata.Note != note {
		t.Errorf("metadata.Note = %q, want %q", metadata.Note, note)
	}
	if metadata.Description != description {
		t.Errorf("metadata.Description = %q, want %q", metadata.Description, description)
	}
	if metadata.OriginalPrompt != originalPrompt {
		t.Errorf("metadata.OriginalPrompt = %q, want %q", metadata.OriginalPrompt, originalPrompt)
	}
	if metadata.LLMMessage != llmMessage {
		t.Errorf("metadata.LLMMessage = %q, want %q", metadata.LLMMessage, llmMessage)
	}
	if metadata.AgentModel != editingModel {
		t.Errorf("metadata.AgentModel = %q, want %q", metadata.AgentModel, editingModel)
	}
}

// TestRecordChangeWithDetails_SpecialCharactersInFilename tests that special characters in filenames are handled.
func TestRecordChangeWithDetails_SpecialCharactersInFilename(t *testing.T) {
	tmpDir := t.TempDir()
	oldChangesDir := changesDir
	oldRevisionsDir := revisionsDir
	defer func() {
		changesDir = oldChangesDir
		revisionsDir = oldRevisionsDir
	}()

	changesDir = filepath.Join(tmpDir, "changes")
	revisionsDir = filepath.Join(tmpDir, "revisions")

	// First record the base revision
	revisionID := "test-revision-special-filename"
	_, err := RecordBaseRevision(revisionID, "Make changes", "Done", nil)
	if err != nil {
		t.Fatalf("RecordBaseRevision() error = %v", err)
	}

	// Record a change with special characters in filename
	filename := "path/to/my-file_v2.0.go"
	originalCode := "package main"
	newCode := "package main\n\nfunc main() {}"
	description := "Add main function"
	note := "Test"

	err = RecordChangeWithDetails(revisionID, filename, originalCode, newCode, description, note, "", "", "")
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error = %v", err)
	}

	// Find the change directory
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		t.Fatalf("failed to read changes dir: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 change directory, got %d", len(entries))
	}

	changeDir := entries[0].Name()

	// Verify files exist with sanitized filename (path separators replaced with _)
	safeFilename := "path_to_my-file_v2.0.go" // path separators replaced
	originalPath := filepath.Join(changesDir, changeDir, safeFilename+".original")
	updatedPath := filepath.Join(changesDir, changeDir, safeFilename+".updated")

	if _, err := os.Stat(originalPath); os.IsNotExist(err) {
		t.Errorf("original file does not exist: %s", originalPath)
	}
	if _, err := os.Stat(updatedPath); os.IsNotExist(err) {
		t.Errorf("updated file does not exist: %s", updatedPath)
	}
}

// TestRecordChangeWithDetails_EmptyDetails tests that RecordChangeWithDetails handles empty optional fields.
func TestRecordChangeWithDetails_EmptyDetails(t *testing.T) {
	tmpDir := t.TempDir()
	oldChangesDir := changesDir
	oldRevisionsDir := revisionsDir
	defer func() {
		changesDir = oldChangesDir
		revisionsDir = oldRevisionsDir
	}()

	changesDir = filepath.Join(tmpDir, "changes")
	revisionsDir = filepath.Join(tmpDir, "revisions")

	// First record the base revision
	revisionID := "test-revision-empty-details"
	_, err := RecordBaseRevision(revisionID, "Make changes", "Done", nil)
	if err != nil {
		t.Fatalf("RecordBaseRevision() error = %v", err)
	}

	// Record a change with empty optional fields
	filename := "test.go"
	originalCode := "package main"
	newCode := "package main\n\nfunc main() {}"
	description := "Add main function"
	note := "Initial"
	originalPrompt := "" // Empty
	llmMessage := ""    // Empty
	editingModel := ""  // Empty

	err = RecordChangeWithDetails(revisionID, filename, originalCode, newCode, description, note, originalPrompt, llmMessage, editingModel)
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error = %v", err)
	}

	// Verify the change was recorded
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges() error = %v", err)
	}

	if len(changes) != 1 {
		t.Errorf("GetAllChanges() got %d changes, want 1", len(changes))
		return
	}

	change := changes[0]
	if change.OriginalPrompt != "" {
		t.Errorf("GetAllChanges() got OriginalPrompt %q, want empty", change.OriginalPrompt)
	}
	if change.LLMMessage != "" {
		t.Errorf("GetAllChanges() got LLMMessage %q, want empty", change.LLMMessage)
	}
	if change.AgentModel != "" {
		t.Errorf("GetAllChanges() got AgentModel %q, want empty", change.AgentModel)
	}
}

// TestRecordChangeWithDetails_MultipleChanges tests that multiple changes can be recorded for the same revision.
func TestRecordChangeWithDetails_MultipleChanges(t *testing.T) {
	tmpDir := t.TempDir()
	oldChangesDir := changesDir
	oldRevisionsDir := revisionsDir
	defer func() {
		changesDir = oldChangesDir
		revisionsDir = oldRevisionsDir
	}()

	changesDir = filepath.Join(tmpDir, "changes")
	revisionsDir = filepath.Join(tmpDir, "revisions")

	// First record the base revision
	revisionID := "test-revision-multiple"
	_, err := RecordBaseRevision(revisionID, "Make changes", "Done", nil)
	if err != nil {
		t.Fatalf("RecordBaseRevision() error = %v", err)
	}

	// Record first change
	filename1 := "file1.go"
	originalCode1 := "package main"
	newCode1 := "package main\n\n// File 1"
	description1 := "Add to file 1"
	note1 := "First change"

	err = RecordChangeWithDetails(revisionID, filename1, originalCode1, newCode1, description1, note1, "", "", "")
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error for change 1: %v", err)
	}

	// Record second change
	filename2 := "file2.go"
	originalCode2 := "package main"
	newCode2 := "package main\n\n// File 2"
	description2 := "Add to file 2"
	note2 := "Second change"

	err = RecordChangeWithDetails(revisionID, filename2, originalCode2, newCode2, description2, note2, "", "", "")
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error for change 2: %v", err)
	}

	// Record third change (same file as first)
	filename3 := "file1.go"
	originalCode3 := "package main\n\n// File 1"
	newCode3 := "package main\n\n// File 1\nfunc Main() {}"
	description3 := "Update file 1"
	note3 := "Third change"

	err = RecordChangeWithDetails(revisionID, filename3, originalCode3, newCode3, description3, note3, "", "", "")
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error for change 3: %v", err)
	}

	// Verify all changes were recorded
	changes, err := GetAllChanges()
	if err != nil {
		t.Fatalf("GetAllChanges() error = %v", err)
	}

	if len(changes) != 3 {
		t.Errorf("GetAllChanges() got %d changes, want 3", len(changes))
		return
	}

	// Verify all changes belong to the same revision
	for i, change := range changes {
		if change.RequestHash != revisionID {
			t.Errorf("GetAllChanges() change %d RequestHash = %q, want %q", i, change.RequestHash, revisionID)
		}
	}
}

// TestRecordChangeWithDetails_ConversationIntegration tests that RecordChangeWithDetails works with conversation history.
func TestRecordChangeWithDetails_ConversationIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	oldChangesDir := changesDir
	oldRevisionsDir := revisionsDir
	defer func() {
		changesDir = oldChangesDir
		revisionsDir = oldRevisionsDir
	}()

	changesDir = filepath.Join(tmpDir, "changes")
	revisionsDir = filepath.Join(tmpDir, "revisions")

	// First record the base revision with conversation
	revisionID := "test-revision-conversation-integration"
	instructions := "Add a new feature"
	response := "Feature added"
	conversation := []APIMessage{
		{Role: "user", Content: "Add a new feature"},
		{Role: "assistant", Content: "Adding feature..."},
	}

	_, err := RecordBaseRevision(revisionID, instructions, response, conversation)
	if err != nil {
		t.Fatalf("RecordBaseRevision() error = %v", err)
	}

	// Record a change with full details
	filename := "feature.go"
	originalCode := "package main"
	newCode := "package main\n\n// Feature"
	description := "Add feature"
	note := "Feature implementation"
	originalPrompt := "Add a new feature"
	llmMessage := "Feature code generated"
	editingModel := "test-model"

	err = RecordChangeWithDetails(revisionID, filename, originalCode, newCode, description, note, originalPrompt, llmMessage, editingModel)
	if err != nil {
		t.Fatalf("RecordChangeWithDetails() error = %v", err)
	}

	// Verify GetRevisionGroups loads conversation
	groups, err := GetRevisionGroups()
	if err != nil {
		t.Fatalf("GetRevisionGroups() error = %v", err)
	}

	if len(groups) != 1 {
		t.Errorf("GetRevisionGroups() got %d groups, want 1", len(groups))
		return
	}

	group := groups[0]
	if group.RevisionID != revisionID {
		t.Errorf("GetRevisionGroups() got RevisionID %q, want %q", group.RevisionID, revisionID)
	}
	if len(group.Conversation) != 2 {
		t.Errorf("GetRevisionGroups() got %d conversation messages, want 2", len(group.Conversation))
	}
	if group.Instructions != instructions {
		t.Errorf("GetRevisionGroups() got Instructions %q, want %q", group.Instructions, instructions)
	}
	if group.Response != response {
		t.Errorf("GetRevisionGroups() got Response %q, want %q", group.Response, response)
	}
}
