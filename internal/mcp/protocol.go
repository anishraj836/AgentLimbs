package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/extractor"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/search"
	"github.com/crawler-monorepo/internal/storage"
)

type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema ToolSchema `json:"inputSchema"`
}

type ToolSchema struct {
	Type       string                 `json:"type"`
	Properties map[string]Property    `json:"properties"`
	Required   []string               `json:"required,omitempty"`
}

type Property struct {
	Type        string              `json:"type"`
	Description string              `json:"description,omitempty"`
	Enum        []string            `json:"enum,omitempty"`
	Default     interface{}         `json:"default,omitempty"`
	Items       *Property           `json:"items,omitempty"`
	Properties  map[string]Property `json:"properties,omitempty"`
}

type CallToolParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type ToolContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

func HandleRPCMessage(raw []byte, client *crawler.Client) ([]byte, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, err
	}

	switch req.Method {
	case "initialize":
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools": map[string]interface{}{},
				},
				"serverInfo": map[string]interface{}{
					"name":    "agent-limbs-mcp",
					"version": "1.0.0",
				},
			},
		}
		return json.Marshal(resp)

	case "tools/list":
		tools := []Tool{
			{
				Name:        "agent_limbs_scrape",
				Description: "Scrape and extract clean Markdown content from a web URL into AgentLimbs search index",
				InputSchema: ToolSchema{
					Type: "object",
					Properties: map[string]Property{
						"url":         {Type: "string", Description: "Target URL to scrape"},
						"mode":        {Type: "string", Description: "Extraction mode", Enum: []string{"clean_rag", "preserve_links", "raw"}},
						"ttl_seconds": {Type: "integer", Description: "Time to live for the scraped document in seconds"},
					},
					Required: []string{"url"},
				},
			},
			{
				Name:        "agent_limbs_hybrid_search",
				Description: "Perform hybrid BM25 and vector RAG search over indexed documents",
				InputSchema: ToolSchema{
					Type: "object",
					Properties: map[string]Property{
						"query": {Type: "string", Description: "Search query"},
						"limit": {Type: "integer", Description: "Maximum results count"},
					},
					Required: []string{"query"},
				},
			},
		}
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{"tools": tools},
		}
		return json.Marshal(resp)

	case "tools/call":
		var params CallToolParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return json.Marshal(JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error:   &RPCError{Code: -32602, Message: "Invalid params"},
			})
		}

		ctx := context.Background()
		var toolResult CallToolResult

		switch params.Name {
		case "agent_limbs_scrape":
			targetURL, _ := params.Arguments["url"].(string)
			if targetURL == "" {
				toolResult = CallToolResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "Missing required argument 'url'"}}}
				break
			}

			if client == nil {
				client = crawler.NewClient()
			}
			res, err := client.Fetch(ctx, targetURL)
			if err != nil || res == nil || res.Response == nil {
				toolResult = CallToolResult{IsError: true, Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Scrape failed: %v", err)}}}
				break
			}

			bodyBytes, err := io.ReadAll(io.LimitReader(res.Response.Body, 10*1024*1024))
			res.Response.Body.Close()
			if err != nil {
				toolResult = CallToolResult{IsError: true, Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Failed to read body: %v", err)}}}
				break
			}

			mode := "clean_rag"
			if m, ok := params.Arguments["mode"].(string); ok && m != "" {
				mode = m
			}

			ttlSeconds := 0
			if val, ok := params.Arguments["ttl_seconds"]; ok && val != nil {
				switch v := val.(type) {
				case float64:
					ttlSeconds = int(v)
				case int:
					ttlSeconds = v
				case string:
					if parsed, err := strconv.Atoi(v); err == nil {
						ttlSeconds = parsed
					}
				}
			}

			markdownContent, _, title := extractor.ConvertHTMLToMarkdown(targetURL, bodyBytes, mode)

			totalTokens := len(markdownContent) / 4
			ttlDuration := time.Duration(ttlSeconds) * time.Second
			_ = storage.SaveCrawledDocumentWithTTL(ctx, res.FinalURL, title, markdownContent, totalTokens, "mcp_scraped", targetURL, ttlDuration)
			_ = index.GlobalEngine.IndexDocumentIncrementalByURL(ctx, res.FinalURL)

			toolResult = CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: fmt.Sprintf("Successfully scraped %s\nTitle: %s\n\nContent:\n%s", res.FinalURL, title, markdownContent)},
				},
			}

		case "agent_limbs_hybrid_search":
			query, _ := params.Arguments["query"].(string)
			if query == "" {
				toolResult = CallToolResult{IsError: true, Content: []ToolContent{{Type: "text", Text: "Missing required argument 'query'"}}}
				break
			}

			limit := 5
			if val, ok := params.Arguments["limit"]; ok && val != nil {
				switch v := val.(type) {
				case float64:
					if int(v) > 0 {
						limit = int(v)
					}
				case float32:
					if int(v) > 0 {
						limit = int(v)
					}
				case int:
					if v > 0 {
						limit = v
					}
				case int64:
					if v > 0 {
						limit = int(v)
					}
				case string:
					if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
						limit = parsed
					}
				case json.Number:
					if parsed, err := v.Int64(); err == nil && parsed > 0 {
						limit = int(parsed)
					}
				}
			}

			if limit > 100 {
				limit = 100
			}

			fetchK := limit
			if fetchK < 10 {
				fetchK = 10
			}

			titles, urls, bodies := index.GlobalEngine.GetMetadataMaps()
			bm25Hits := index.GlobalEngine.Inverted.RankDocuments(
				query,
				titles,
				urls,
				bodies,
				fetchK,
			)
			vectorHits := index.GlobalEngine.SearchVector(query, fetchK)
			fusedHits := search.ReciprocalRankFusion(query, bm25Hits, vectorHits, limit, titles, urls, bodies)

			resJSON, _ := json.MarshalIndent(fusedHits, "", "  ")
			toolResult = CallToolResult{
				Content: []ToolContent{
					{Type: "text", Text: string(resJSON)},
				},
			}

		default:
			toolResult = CallToolResult{IsError: true, Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("Unknown tool: %s", params.Name)}}}
		}

		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  toolResult,
		}
		return json.Marshal(resp)

	default:
		// Ignore notifications or unknown methods
		if req.ID == nil {
			return nil, nil
		}
		resp := JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: -32601, Message: "Method not found"},
		}
		return json.Marshal(resp)
	}
}
