package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureMCP_NewFileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	userConfigDir := filepath.Join(tmpDir, "config")
	homeDir := filepath.Join(tmpDir, "home")
	cwd := filepath.Join(tmpDir, "workspace")

	opts := ConfigOptions{
		Editor:        "all",
		BinaryPath:    "/usr/local/bin/agentlimbs",
		UserConfigDir: userConfigDir,
		HomeDir:       homeDir,
		Cwd:           cwd,
	}

	res, err := ConfigureMCP(opts)
	if err != nil {
		t.Fatalf("ConfigureMCP failed: %v", err)
	}

	if len(res.FilesCreated) != 2 {
		t.Fatalf("expected 2 files created, got %d: %v", len(res.FilesCreated), res.FilesCreated)
	}

	// Verify Claude file
	claudePath := filepath.Join(userConfigDir, "Claude", "claude_desktop_config.json")
	claudeBytes, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed reading claude file: %v", err)
	}

	var claudeRoot map[string]any
	if err := json.Unmarshal(claudeBytes, &claudeRoot); err != nil {
		t.Fatalf("invalid claude json: %v", err)
	}
	servers := claudeRoot["mcpServers"].(map[string]any)
	weblimbServer := servers["weblimb"].(map[string]any)
	if weblimbServer["command"] != "/usr/local/bin/agentlimbs" {
		t.Errorf("expected command '/usr/local/bin/agentlimbs', got %v", weblimbServer["command"])
	}

	// Verify Cursor global file
	cursorPath := filepath.Join(homeDir, ".cursor", "mcp.json")
	cursorBytes, err := os.ReadFile(cursorPath)
	if err != nil {
		t.Fatalf("failed reading cursor file: %v", err)
	}
	var cursorRoot map[string]any
	if err := json.Unmarshal(cursorBytes, &cursorRoot); err != nil {
		t.Fatalf("invalid cursor json: %v", err)
	}
	cursorServers := cursorRoot["mcpServers"].(map[string]any)
	if cursorServers["weblimb"] == nil {
		t.Errorf("weblimb server entry missing in cursor config")
	}
}

func TestConfigureMCP_NonDestructiveMergeAndKeyPreservation(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "config", "Claude")
	_ = os.MkdirAll(claudeDir, 0755)
	claudePath := filepath.Join(claudeDir, "claude_desktop_config.json")

	initialJSON := `{
  "$schema": "https://json.schemastore.org/claude-desktop-config.json",
  "theme": "dark",
  "telemetry": false,
  "mcpServers": {
    "existing-server": {
      "command": "node",
      "args": ["server.js"]
    }
  }
}`
	if err := os.WriteFile(claudePath, []byte(initialJSON), 0600); err != nil {
		t.Fatalf("failed creating initial config: %v", err)
	}

	opts := ConfigOptions{
		Editor:        "claude",
		BinaryPath:    "/opt/bin/agentlimbs",
		UserConfigDir: filepath.Join(tmpDir, "config"),
	}

	res, err := ConfigureMCP(opts)
	if err != nil {
		t.Fatalf("ConfigureMCP failed: %v", err)
	}

	if len(res.FilesUpdated) != 1 || res.FilesUpdated[0] != claudePath {
		t.Errorf("expected claudePath in FilesUpdated, got: %v", res.FilesUpdated)
	}

	if len(res.BackupsCreated) != 1 || res.BackupsCreated[0] != claudePath+".bak" {
		t.Errorf("expected backup file %s.bak, got: %v", claudePath, res.BackupsCreated)
	}

	// Verify backup content is exactly original
	bakBytes, err := os.ReadFile(claudePath + ".bak")
	if err != nil {
		t.Fatalf("failed to read backup file: %v", err)
	}
	if string(bakBytes) != initialJSON {
		t.Errorf("backup content does not match original file")
	}

	// Verify merged content
	mergedBytes, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("failed to read merged file: %v", err)
	}

	var mergedRoot map[string]any
	if err := json.Unmarshal(mergedBytes, &mergedRoot); err != nil {
		t.Fatalf("failed unmarshaling merged json: %v", err)
	}

	// Verify top-level non-MCP keys are preserved
	if mergedRoot["$schema"] != "https://json.schemastore.org/claude-desktop-config.json" {
		t.Errorf("expected $schema preserved, got %v", mergedRoot["$schema"])
	}
	if mergedRoot["theme"] != "dark" {
		t.Errorf("expected theme preserved, got %v", mergedRoot["theme"])
	}
	if mergedRoot["telemetry"] != false {
		t.Errorf("expected telemetry preserved, got %v", mergedRoot["telemetry"])
	}

	// Verify both servers exist in mcpServers
	servers := mergedRoot["mcpServers"].(map[string]any)
	if servers["existing-server"] == nil {
		t.Errorf("expected existing-server preserved")
	}
	if servers["weblimb"] == nil {
		t.Errorf("expected weblimb added")
	}

	// Verify file permissions preserved (0600)
	info, statErr := os.Stat(claudePath)
	if statErr != nil {
		t.Fatalf("stat failed: %v", statErr)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected file mode 0600, got %o", info.Mode().Perm())
	}
}

func TestConfigureMCP_EmptyFileHandling(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	cursorDir := filepath.Join(homeDir, ".cursor")
	_ = os.MkdirAll(cursorDir, 0755)
	cursorPath := filepath.Join(cursorDir, "mcp.json")

	// Create 0-byte file
	if err := os.WriteFile(cursorPath, []byte(""), 0644); err != nil {
		t.Fatalf("failed creating empty file: %v", err)
	}

	opts := ConfigOptions{
		Editor:     "cursor",
		BinaryPath: "/usr/local/bin/agentlimbs",
		HomeDir:    homeDir,
	}

	res, err := ConfigureMCP(opts)
	if err != nil {
		t.Fatalf("ConfigureMCP failed: %v", err)
	}

	if len(res.FilesUpdated) != 1 {
		t.Errorf("expected empty file to be updated, got: %v", res.FilesUpdated)
	}

	content, err := os.ReadFile(cursorPath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}

	var root map[string]any
	if err := json.Unmarshal(content, &root); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	servers := root["mcpServers"].(map[string]any)
	if servers["weblimb"] == nil {
		t.Errorf("expected weblimb server entry created")
	}
}

func TestConfigureMCP_PristineBackupNotOverwritten(t *testing.T) {
	tmpDir := t.TempDir()
	claudeDir := filepath.Join(tmpDir, "config", "Claude")
	_ = os.MkdirAll(claudeDir, 0755)
	claudePath := filepath.Join(claudeDir, "claude_desktop_config.json")
	bakPath := claudePath + ".bak"

	pristineContent := `{"original": "pristine"}`
	secondContent := `{"mcpServers": {"v1": {}}}`

	_ = os.WriteFile(claudePath, []byte(secondContent), 0644)
	_ = os.WriteFile(bakPath, []byte(pristineContent), 0644)

	opts := ConfigOptions{
		Editor:        "claude",
		BinaryPath:    "/bin/agentlimbs",
		UserConfigDir: filepath.Join(tmpDir, "config"),
	}

	res, err := ConfigureMCP(opts)
	if err != nil {
		t.Fatalf("ConfigureMCP failed: %v", err)
	}

	// Backup should not be overwritten
	if len(res.BackupsCreated) != 0 {
		t.Errorf("expected no backup created when .bak already exists, got: %v", res.BackupsCreated)
	}

	bakBytes, _ := os.ReadFile(bakPath)
	if string(bakBytes) != pristineContent {
		t.Errorf("existing .bak was modified, expected %q, got %q", pristineContent, string(bakBytes))
	}
}

func TestConfigureMCP_DryRunAndStdout(t *testing.T) {
	tmpDir := t.TempDir()
	homeDir := filepath.Join(tmpDir, "home")
	cursorDir := filepath.Join(homeDir, ".cursor")
	_ = os.MkdirAll(cursorDir, 0755)
	cursorPath := filepath.Join(cursorDir, "mcp.json")
	_ = os.WriteFile(cursorPath, []byte(`{"initial": true}`), 0644)

	// Test DryRun
	optsDry := ConfigOptions{
		Editor:     "cursor",
		BinaryPath: "/bin/agentlimbs",
		HomeDir:    homeDir,
		DryRun:     true,
	}

	resDry, err := ConfigureMCP(optsDry)
	if err != nil {
		t.Fatalf("DryRun ConfigureMCP failed: %v", err)
	}

	if len(resDry.DryRunDiffs) != 1 {
		t.Errorf("expected 1 DryRunDiff, got %d", len(resDry.DryRunDiffs))
	}

	// Verify disk file was NOT modified
	diskBytes, _ := os.ReadFile(cursorPath)
	if string(diskBytes) != `{"initial": true}` {
		t.Errorf("DryRun modified file on disk")
	}

	// Test Stdout
	optsStdout := ConfigOptions{
		BinaryPath: "/bin/agentlimbs",
		Stdout:     true,
	}

	resStdout, err := ConfigureMCP(optsStdout)
	if err != nil {
		t.Fatalf("Stdout ConfigureMCP failed: %v", err)
	}

	if !strings.Contains(resStdout.StdoutJSON, "weblimb") {
		t.Errorf("expected weblimb in StdoutJSON, got %s", resStdout.StdoutJSON)
	}
	if len(resStdout.FilesCreated) > 0 || len(resStdout.FilesUpdated) > 0 {
		t.Errorf("Stdout mode should not touch files")
	}
}

func TestConfigureMCP_WorkspaceCursor(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "my-project")
	_ = os.MkdirAll(workspaceDir, 0755)

	opts := ConfigOptions{
		Editor:     "cursor",
		BinaryPath: "/bin/agentlimbs",
		Workspace:  true,
		Cwd:        workspaceDir,
	}

	res, err := ConfigureMCP(opts)
	if err != nil {
		t.Fatalf("Workspace ConfigureMCP failed: %v", err)
	}

	expectedPath := filepath.Join(workspaceDir, ".cursor", "mcp.json")
	if len(res.FilesCreated) != 1 || res.FilesCreated[0] != expectedPath {
		t.Errorf("expected workspace file created at %s, got: %v", expectedPath, res.FilesCreated)
	}

	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("workspace mcp.json was not created on disk")
	}
}

func TestResolveBinaryPath_OverrideAndLookPath(t *testing.T) {
	// Test explicit override
	path, warnings := ResolveBinaryPath("/custom/bin/agentlimbs")
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings for override: %v", warnings)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got: %s", path)
	}
}
