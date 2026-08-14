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

	foundHybridSearch := false
	for _, tool := range listResp.Result.Tools {
		if tool.Name == "agent_limbs_hybrid_search" {
			foundHybridSearch = true
			limitProp, exists := tool.InputSchema.Properties["limit"]
			if !exists {
				t.Fatalf("expected 'limit' property in agent_limbs_hybrid_search schema")
			}
			if limitProp.Type != "integer" {
				t.Fatalf("expected 'limit' property type to be 'integer', got '%s'", limitProp.Type)
			}
		}
	}
	if !foundHybridSearch {
		t.Fatalf("agent_limbs_hybrid_search tool not found in list")
	}
}

func TestMCPHybridSearchLimit(t *testing.T) {
	// Test calling agent_limbs_hybrid_search with different limit formats (omitted, integer, string)
	tests := []struct {
		name string
		req  string
	}{
		{
			name: "Omitted limit (default 5)",
			req:  `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_limbs_hybrid_search","arguments":{"query":"golang"}}}`,
		},
		{
			name: "Integer limit",
			req:  `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"agent_limbs_hybrid_search","arguments":{"query":"golang","limit":3}}}`,
		},
		{
			name: "String limit",
			req:  `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"agent_limbs_hybrid_search","arguments":{"query":"golang","limit":"2"}}}`,
		},
		{
			name: "Large limit capped at 100",
			req:  `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"agent_limbs_hybrid_search","arguments":{"query":"golang","limit":500}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			respBytes, err := HandleRPCMessage([]byte(tt.req), nil)
			if err != nil {
				t.Fatalf("HandleRPCMessage failed: %v", err)
			}

			var resp struct {
				JSONRPC string         `json:"jsonrpc"`
				ID      int            `json:"id"`
				Result  CallToolResult `json:"result"`
			}
			if err := json.Unmarshal(respBytes, &resp); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}
			if resp.Result.IsError {
				t.Fatalf("unexpected tool call error: %v", resp.Result.Content)
			}
		})
	}
}

func TestMCPHybridSearchNegativeLimit(t *testing.T) {
	req := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"agent_limbs_hybrid_search","arguments":{"query":"golang","limit":-5}}}`
	respBytes, err := HandleRPCMessage([]byte(req), nil)
	if err != nil {
		t.Fatalf("HandleRPCMessage failed: %v", err)
	}

	var resp struct {
		JSONRPC string         `json:"jsonrpc"`
		ID      int            `json:"id"`
		Result  CallToolResult `json:"result"`
	}
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if !resp.Result.IsError {
		t.Fatalf("Expected IsError: true for negative limit, got false")
	}

	expectedText := "Invalid limit parameter: limit must be a non-negative integer (got -5)"
	if len(resp.Result.Content) == 0 || resp.Result.Content[0].Text != expectedText {
		t.Fatalf("Expected error text '%s', got '%v'", expectedText, resp.Result.Content)
	}
}

func TestMCPProtocol_ParseError(t *testing.T) {
	invalidJSON := `{"jsonrpc": "2.0", "method": `
	respBytes, err := HandleRPCMessage([]byte(invalidJSON), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("failed to unmarshal parse error response: %v", err)
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Fatalf("expected error code -32700, got: %v", resp.Error)
	}
}
