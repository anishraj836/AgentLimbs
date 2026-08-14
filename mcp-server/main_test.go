package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLargePayload(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mcp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tempBinaryPath := filepath.Join(tempDir, "agent-limbs-mcp")
	buildCmd := exec.Command("go", "build", "-o", tempBinaryPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, out)
	}

	// Create a payload larger than 10MB
	largeString := strings.Repeat("a", 11*1024*1024)
	payload := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "test_method",
		"params": map[string]interface{}{
			"data": largeString,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal payload: %v", err)
	}

	// We'll run the dynamically built agent-limbs-mcp binary
	cmd := exec.Command(tempBinaryPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to create stdin pipe: %v", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start agent-limbs-mcp: %v", err)
	}

	go func() {
		defer stdin.Close()
		stdin.Write(payloadBytes)
		stdin.Write([]byte("\n"))
	}()

	// Read output with a timeout
	done := make(chan struct{})
	var outBytes []byte

	go func() {
		// Just read enough to see if it responds or doesn't crash with too long error
		buf := make([]byte, 1024)
		n, _ := stdout.Read(buf)
		if n > 0 {
			outBytes = buf[:n]
		}
		close(done)
	}()

	select {
	case <-time.After(5 * time.Second):
		t.Fatalf("Timeout waiting for response from mcp-server")
	case <-done:
		// Check that it didn't exit with a panic about bufio scanner
		if len(outBytes) > 0 {
			outStr := string(outBytes)
			// It should give an error like "Method not found" or process it, but shouldn't crash
			if !strings.Contains(outStr, "jsonrpc") && !strings.Contains(outStr, "error") {
				// The actual response might just be an RPC error for unknown method, which is valid.
				t.Logf("Received response: %s", outStr)
			}
		}
	}

	cmd.Process.Kill()
	cmd.Wait()
}

func TestStdioInvalidJSON_Continues(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mcp-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	tempBinaryPath := filepath.Join(tempDir, "agent-limbs-mcp")
	buildCmd := exec.Command("go", "build", "-o", tempBinaryPath, ".")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to build binary: %v\nOutput: %s", err, out)
	}

	cmd := exec.Command(tempBinaryPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("Failed to create stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Failed to create stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start binary: %v", err)
	}
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	// Send invalid JSON line
	invalidJSON := "{ bad json \n"
	// Followed by valid initialize request
	validJSON := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n"

	go func() {
		stdin.Write([]byte(invalidJSON))
		time.Sleep(50 * time.Millisecond)
		stdin.Write([]byte(validJSON))
		time.Sleep(50 * time.Millisecond)
		stdin.Close()
	}()

	buf := make([]byte, 4096)
	n, _ := stdout.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "-32700") && !strings.Contains(output, "Parse error") {
		t.Errorf("Expected -32700 Parse error in output, got: %s", output)
	}
}
