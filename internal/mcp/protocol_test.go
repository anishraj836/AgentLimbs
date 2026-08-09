package mcp

import (
	"encoding/json"
	"testing"
)

func TestMCPProtocolInitializeAndList(t *testing.T) {
	// 1. Test initialize
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	respBytes, err := HandleRPCMessage([]byte(initReq), nil)
	if err != nil {
		t.Fatalf("failed to handle initialize: %v", err)
	}

	var initResp JSONRPCResponse
	if err := json.Unmarshal(respBytes, &initResp); err != nil {
		t.Fatalf("failed to unmarshal initialize response: %v", err)
	}
	if initResp.Error != nil {
		t.Fatalf("unexpected error in initialize response: %v", initResp.Error.Message)
	}

	// 2. Test tools/list
	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	respBytes, err = HandleRPCMessage([]byte(listReq), nil)
	if err != nil {
		t.Fatalf("failed to handle tools/list: %v", err)
	}

	var listResp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBytes, &listResp); err != nil {
		t.Fatalf("failed to unmarshal tools/list response: %v", err)
	}

	if len(listResp.Result.Tools) < 2 {
		t.Fatalf("expected at least 2 MCP tools registered, got %d", len(listResp.Result.Tools))
	}
}
