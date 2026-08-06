package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPInitialize(t *testing.T) {
	reqJSON := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	respBytes, err := HandleRPCMessage([]byte(reqJSON), nil)
	if err != nil {
		t.Fatalf("HandleRPCMessage failed: %v", err)
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Error != nil {
		t.Fatalf("expected no RPC error, got: %v", resp.Error)
	}

	resMap, ok := resp.Result.(map[string]interface{})
	if !ok || resMap["protocolVersion"] != "2024-11-05" {
		t.Errorf("expected protocolVersion 2024-11-05, got %v", resMap["protocolVersion"])
	}
}

func TestMCPToolsList(t *testing.T) {
	reqJSON := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	respBytes, err := HandleRPCMessage([]byte(reqJSON), nil)
	if err != nil {
		t.Fatalf("HandleRPCMessage failed: %v", err)
	}

	var resp JSONRPCResponse
	json.Unmarshal(respBytes, &resp)

	resMap := resp.Result.(map[string]interface{})
	toolsList := resMap["tools"].([]interface{})

	if len(toolsList) < 2 {
		t.Errorf("expected at least 2 MCP tools, got %d", len(toolsList))
	}
}

func TestMCPResourcesList(t *testing.T) {
	reqJSON := `{"jsonrpc":"2.0","id":3,"method":"resources/list"}`
	respBytes, err := HandleRPCMessage([]byte(reqJSON), nil)
	if err != nil {
		t.Fatalf("HandleRPCMessage failed: %v", err)
	}

	var resp JSONRPCResponse
	json.Unmarshal(respBytes, &resp)

	resMap := resp.Result.(map[string]interface{})
	resList := resMap["resources"].([]interface{})

	if len(resList) == 0 {
		t.Errorf("expected MCP resources, got 0")
	}
}

func TestMCPToolsCallSearch(t *testing.T) {
	reqJSON := `{
		"jsonrpc": "2.0",
		"id": 4,
		"method": "tools/call",
		"params": {
			"name": "agent_limbs_hybrid_search",
			"arguments": {
				"query": "golang concurrency",
				"top_k": 5
			}
		}
	}`

	respBytes, err := HandleRPCMessage([]byte(reqJSON), nil)
	if err != nil {
		t.Fatalf("HandleRPCMessage failed: %v", err)
	}

	var resp JSONRPCResponse
	json.Unmarshal(respBytes, &resp)

	resMap := resp.Result.(map[string]interface{})
	contentList := resMap["content"].([]interface{})
	firstContent := contentList[0].(map[string]interface{})
	text := firstContent["text"].(string)

	if !strings.Contains(text, "Hybrid Search Results") {
		t.Errorf("expected search output, got:\n%s", text)
	}
}
