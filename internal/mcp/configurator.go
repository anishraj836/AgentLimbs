package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ConfigOptions encapsulates options for configuring AI IDE MCP settings.
type ConfigOptions struct {
	// Editor to configure: "all" (default), "cursor", or "claude"
	Editor string
	// BinaryPath explicitly overrides the agentlimbs binary executable path
	BinaryPath string
	// DryRun previews the JSON diff without writing to disk
	DryRun bool
	// Stdout indicates whether to output the JSON configuration block for manual copying
	Stdout bool
	// Global indicates Cursor global configuration (~/.cursor/mcp.json)
	Global bool
	// Workspace indicates Cursor workspace configuration (.cursor/mcp.json)
	Workspace bool
	// Cwd overrides current working directory (useful for testing)
	Cwd string
	// UserConfigDir overrides os.UserConfigDir (useful for testing)
	UserConfigDir string
	// HomeDir overrides os.UserHomeDir (useful for testing)
	HomeDir string
	// CustomArgs overrides the default MCP arguments ["serve", "--mcp"]
	CustomArgs []string
}

// ConfigResult contains the outcome of the MCP configuration process.
type ConfigResult struct {
	ResolvedBinaryPath string            `json:"resolved_binary_path"`
	FilesUpdated       []string          `json:"files_updated,omitempty"`
	FilesCreated       []string          `json:"files_created,omitempty"`
	BackupsCreated     []string          `json:"backups_created,omitempty"`
	DryRunDiffs        map[string]string `json:"dry_run_diffs,omitempty"`
	StdoutJSON         string            `json:"stdout_json,omitempty"`
	Warnings           []string          `json:"warnings,omitempty"`
}

// ResolveBinaryPath determines the absolute path to the agentlimbs binary.
// It detects ephemeral `go run` or `go-build` temporary paths and attempts
// to look up `lightlimbs`, `weblimb`, or `agentlimbs` in PATH.
func ResolveBinaryPath(override string) (string, []string) {
	var warnings []string

	if override != "" {
		if abs, err := filepath.Abs(override); err == nil {
			return abs, warnings
		}
		return override, warnings
	}

	execPath, err := os.Executable()
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Unable to determine executable path (%v); defaulting to 'lightlimbs'", err))
		return "lightlimbs", warnings
	}

	// Resolve symlinks if possible
	if evaluated, evalErr := filepath.EvalSymlinks(execPath); evalErr == nil {
		execPath = evaluated
	}

	// Detect ephemeral `go run` / `go-build` temporary paths
	lowerExec := strings.ToLower(execPath)
	isEphemeral := strings.Contains(lowerExec, "go-build") ||
		strings.Contains(lowerExec, filepath.Join("tmp", "go-build")) ||
		strings.Contains(lowerExec, "/var/folders/") ||
		strings.Contains(lowerExec, "\\appdata\\local\\temp\\")

	if isEphemeral {
		// Attempt to look for installed lightlimbs, weblimb, or agentlimbs in PATH
		candidateNames := []string{"lightlimbs", "weblimb", "agentlimbs", "agentlimbs-light", "agent-limbs-mcp"}
		foundInPath := false
		for _, name := range candidateNames {
			if lp, lpErr := exec.LookPath(name); lpErr == nil {
				if absLP, absErr := filepath.Abs(lp); absErr == nil {
					execPath = absLP
					foundInPath = true
					break
				}
			}
		}

		if !foundInPath {
			warnings = append(warnings, fmt.Sprintf(
				"Detected ephemeral go-build binary path (%s). MCP configuration will point to this temporary binary. "+
					"For permanent installation, install agentlimbs to your PATH (e.g., /usr/local/bin/agentlimbs) or pass --binary-path.",
				execPath,
			))
		}
	}

	if abs, err := filepath.Abs(execPath); err == nil {
		execPath = abs
	}

	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(execPath), ".exe") {
		execPath += ".exe"
	}

	return execPath, warnings
}

// GetClaudeConfigPath returns the cross-platform path to the Claude Desktop config.
func GetClaudeConfigPath(userConfigDir string) (string, error) {
	if userConfigDir == "" {
		var err error
		userConfigDir, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user config directory: %w", err)
		}
	}
	return filepath.Join(userConfigDir, "Claude", "claude_desktop_config.json"), nil
}

// GetCursorConfigPath returns the Cursor MCP configuration path (global or workspace).
func GetCursorConfigPath(isWorkspace bool, homeDir, cwd string) (string, error) {
	if isWorkspace {
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return "", fmt.Errorf("failed to get current working directory: %w", err)
			}
		}
		return filepath.Join(cwd, ".cursor", "mcp.json"), nil
	}

	if homeDir == "" {
		var err error
		homeDir, err = os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
	}
	return filepath.Join(homeDir, ".cursor", "mcp.json"), nil
}

// ConfigureMCP orchestrates non-destructive MCP auto-configuration for Claude Desktop and Cursor IDE.
func ConfigureMCP(opts ConfigOptions) (*ConfigResult, error) {
	binPath, warnings := ResolveBinaryPath(opts.BinaryPath)
	result := &ConfigResult{
		ResolvedBinaryPath: binPath,
		DryRunDiffs:        make(map[string]string),
		Warnings:           warnings,
	}

	args := opts.CustomArgs
	if len(args) == 0 {
		args = []string{"serve", "--mcp"}
	}

	mcpServerBlock := map[string]any{
		"command": binPath,
		"args":    args,
	}

	stdoutConfig := map[string]any{
		"mcpServers": map[string]any{
			"weblimb": mcpServerBlock,
		},
	}
	stdoutBytes, _ := json.MarshalIndent(stdoutConfig, "", "  ")
	result.StdoutJSON = string(stdoutBytes)

	// If stdout only requested, we don't need to write to disk
	if opts.Stdout {
		return result, nil
	}

	// Determine target paths based on editor option
	editor := strings.ToLower(strings.TrimSpace(opts.Editor))
	if editor == "" {
		editor = "all"
	}

	type targetConfig struct {
		Name string
		Path string
	}

	var targets []targetConfig

	if editor == "all" || editor == "claude" {
		claudePath, err := GetClaudeConfigPath(opts.UserConfigDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Could not determine Claude config path: %v", err))
		} else {
			targets = append(targets, targetConfig{Name: "Claude Desktop", Path: claudePath})
		}
	}

	if editor == "all" || editor == "cursor" {
		cursorPath, err := GetCursorConfigPath(opts.Workspace, opts.HomeDir, opts.Cwd)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Could not determine Cursor config path: %v", err))
		} else {
			name := "Cursor (Global)"
			if opts.Workspace {
				name = "Cursor (Workspace)"
			}
			targets = append(targets, targetConfig{Name: name, Path: cursorPath})
		}
	}

	for _, target := range targets {
		if err := configureSingleTarget(target.Path, mcpServerBlock, opts.DryRun, result); err != nil {
			return result, fmt.Errorf("failed configuring %s at %s: %w", target.Name, target.Path, err)
		}
	}

	return result, nil
}

func configureSingleTarget(targetPath string, mcpServerBlock map[string]any, dryRun bool, result *ConfigResult) error {
	var root map[string]any
	fileExisted := false
	mode := os.FileMode(0644)

	info, err := os.Stat(targetPath)
	if err == nil {
		fileExisted = true
		mode = info.Mode().Perm()
		data, readErr := os.ReadFile(targetPath)
		if readErr != nil {
			return fmt.Errorf("failed to read target file %s: %w", targetPath, readErr)
		}

		trimmed := strings.TrimSpace(string(data))
		if len(trimmed) == 0 {
			root = make(map[string]any)
		} else {
			if unmarshalErr := json.Unmarshal(data, &root); unmarshalErr != nil {
				return fmt.Errorf("target file %s contains invalid JSON: %w", targetPath, unmarshalErr)
			}
		}
	} else if os.IsNotExist(err) {
		root = make(map[string]any)
	} else {
		return fmt.Errorf("failed to inspect target file %s: %w", targetPath, err)
	}

	if root == nil {
		root = make(map[string]any)
	}

	// Preserve all other keys and merge into mcpServers map
	var servers map[string]any
	if s, ok := root["mcpServers"].(map[string]any); ok && s != nil {
		servers = s
	} else {
		servers = make(map[string]any)
		root["mcpServers"] = servers
	}

	servers["weblimb"] = mcpServerBlock

	formattedJSON, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode merged JSON for %s: %w", targetPath, err)
	}
	formattedJSON = append(formattedJSON, '\n')

	if dryRun {
		result.DryRunDiffs[targetPath] = string(formattedJSON)
		if fileExisted {
			result.FilesUpdated = append(result.FilesUpdated, targetPath)
		} else {
			result.FilesCreated = append(result.FilesCreated, targetPath)
		}
		return nil
	}

	// Ensure parent directory exists
	targetDir := filepath.Dir(targetPath)
	if mkdirErr := os.MkdirAll(targetDir, 0755); mkdirErr != nil {
		return fmt.Errorf("failed to create directory %s: %w", targetDir, mkdirErr)
	}

	// Create backup only if file exists, is non-empty, and .bak does not already exist
	if fileExisted {
		bakPath := targetPath + ".bak"
		if _, bakStatErr := os.Stat(bakPath); os.IsNotExist(bakStatErr) {
			origBytes, readOrigErr := os.ReadFile(targetPath)
			if readOrigErr == nil && len(strings.TrimSpace(string(origBytes))) > 0 {
				if writeBakErr := os.WriteFile(bakPath, origBytes, mode); writeBakErr == nil {
					result.BackupsCreated = append(result.BackupsCreated, bakPath)
				}
			}
		}
	}

	// Atomic Co-located Write: Create temporary file in filepath.Dir(targetPath) to prevent EXDEV errors
	tmpFile, err := os.CreateTemp(targetDir, ".mcp_cfg_*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s: %w", targetDir, err)
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(formattedJSON); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write config to temp file: %w", err)
	}

	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Chmod(tmpName, mode); err != nil {
		return fmt.Errorf("failed to preserve file permissions on %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", targetPath, err)
	}

	if fileExisted {
		result.FilesUpdated = append(result.FilesUpdated, targetPath)
	} else {
		result.FilesCreated = append(result.FilesCreated, targetPath)
	}

	return nil
}
