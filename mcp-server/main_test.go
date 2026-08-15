package main_test

import (
	"bufio"
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
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	validJSON := `{"jsonrpc":"2.0","id":2,"method":"initialize"}` + "\n"

	go func() {
		_, _ = stdin.Write(payloadBytes)
		_, _ = stdin.Write([]byte("\n"))
		time.Sleep(100 * time.Millisecond)
		_, _ = stdin.Write([]byte(validJSON))
		_ = stdin.Close()
	}()

	outChan := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			if strings.Contains(scanner.Text(), "-32700") {
				break
			}
		}
		outChan <- strings.Join(lines, "\n")
	}()

	select {
	case outStr := <-outChan:
		if !strings.Contains(outStr, "-32700") || !strings.Contains(outStr, "line length exceeded 10MB") {
			t.Errorf("expected line length exceeded 10MB error response, got: %s", outStr)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for MCP response on large payload")
	}
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
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Send invalid JSON line followed by valid initialize request
	invalidJSON := "{ bad json \n"
	validJSON := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n"

	go func() {
		_, _ = stdin.Write([]byte(invalidJSON))
		time.Sleep(100 * time.Millisecond)
		_, _ = stdin.Write([]byte(validJSON))
		_ = stdin.Close()
	}()

	outChan := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		var lines []string
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
			if strings.Contains(scanner.Text(), "-32700") {
				break
			}
		}
		outChan <- strings.Join(lines, "\n")
	}()

	select {
	case output := <-outChan:
		if !strings.Contains(output, "-32700") && !strings.Contains(output, "Parse error") {
			t.Errorf("Expected -32700 Parse error in output, got: %s", output)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for MCP error response")
	}
}
