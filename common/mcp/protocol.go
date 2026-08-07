package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/crawler-monorepo/common/bm25"
	"github.com/crawler-monorepo/common/hybrid"
	"github.com/crawler-monorepo/common/markdown"
	"github.com/crawler-monorepo/common/utils"
	"github.com/crawler-monorepo/common/vector"
	"github.com/crawler-monorepo/crawler-service/httpclient"
	"github.com/crawler-monorepo/document-processor/processor"
	"github.com/crawler-monorepo/embedding-service/embedder"
	"github.com/crawler-monorepo/indexer-service/indexer"
	"github.com/crawler-monorepo/tokenizer-service/tokenizer"
)

// JSONRPCRequest represents an incoming MCP JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents an outgoing MCP JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// HandleRPCMessage processes incoming MCP JSON-RPC requests and returns the formatted response.
func HandleRPCMessage(reqBytes []byte, client *httpclient.Client) ([]byte, error) {
	var req JSONRPCRequest
	if err := json.Unmarshal(reqBytes, &req); err != nil {
		return json.Marshal(JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: -32700, Message: "Parse error"},
		})
	}

	var result interface{}
	var rpcErr *RPCError

	switch req.Method {
	case "notifications/initialized":
		if req.ID == nil {
			return nil, nil // Notifications do not send RPC responses
		}
		result = map[string]interface{}{}

	case "initialize":
		result = map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{},
				"resources": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "AgentLimbs MCP Server",
				"version": "1.0.0",
			},
		}

	case "tools/list":
		result = map[string]interface{}{
			"tools": []map[string]interface{}{
				{
					"name":        "agent_limbs_scrape",
					"description": "Fetch and convert any website URL into clean, token-efficient Github-Flavored Markdown for LLMs.",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"url": map[string]interface{}{
								"type":        "string",
								"description": "The target website URL to scrape (e.g. https://golang.org)",
							},
							"mode": map[string]interface{}{
								"type":        "string",
								"description": "Extraction mode: 'clean_rag' (default, max token savings), 'preserve_links' (keep all URLs), or 'raw' (minimal filtering)",
							},
						},
						"required": []string{"url"},
					},
				},
				{
					"name":        "agent_limbs_hybrid_search",
					"description": "Search the indexed web corpus using Hybrid RRF (BM25 Keyword + AI Vector Semantic Similarity).",
					"inputSchema": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"query": map[string]interface{}{
								"type":        "string",
								"description": "The natural language search query or topic",
							},
							"top_k": map[string]interface{}{
								"type":        "integer",
								"description": "Number of top ranked results to return (default 5)",
							},
						},
						"required": []string{"query"},
					},
				},
			},
		}

	case "tools/call":
		var callParams struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			rpcErr = &RPCError{Code: -32602, Message: "Invalid params"}
			break
		}

		switch callParams.Name {
		case "agent_limbs_scrape":
			rawURL, _ := callParams.Arguments["url"].(string)
			if rawURL == "" {
				rpcErr = &RPCError{Code: -32602, Message: "Missing url argument"}
				break
			}

			normURL, err := utils.NormalizeURL(rawURL)
			if err != nil {
				rpcErr = &RPCError{Code: -32602, Message: "Invalid URL: " + err.Error()}
				break
			}

			if client == nil {
				client = httpclient.NewClient()
			}

			ctx := context.Background()
			fetchURL := utils.TransformGitHubURL(normURL)
			res, err := client.Fetch(ctx, fetchURL)
			if err != nil {
				rpcErr = &RPCError{Code: -32000, Message: "Fetch failed: " + err.Error()}
				break
			}
			defer res.Response.Body.Close()

			limitedBody := io.LimitReader(res.Response.Body, 10*1024*1024)
			htmlBytes, _ := io.ReadAll(limitedBody)

			mode, _ := callParams.Arguments["mode"].(string)
			mdText, tokens, title := markdown.ConvertHTMLToMarkdownWithMode(res.FinalURL, htmlBytes, mode)

			// Auto-ingest scraped page into Document Processor, Tokenizer, Inverted Index, and Vector Store
			cleanDoc, _ := processor.ProcessRawHTML(res.FinalURL, htmlBytes)
			tokenizedDoc := tokenizer.TokenizePipeline(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body)
			indexer.GlobalEngine.IndexDocument(
				tokenizedDoc.URL,
				tokenizedDoc.Title,
				tokenizedDoc.CleanBody,
				tokenizedDoc.TermPositions,
				tokenizedDoc.TotalTokens,
			)
			embedder.IndexDocumentVector(cleanDoc.URL, cleanDoc.Title, cleanDoc.Body)

			outText := fmt.Sprintf("# Title: %s\n# URL: %s\n# Tokens: %d\n\n%s", title, res.FinalURL, tokens, mdText)

			result = map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": outText,
					},
				},
			}

		case "agent_limbs_hybrid_search":
			query, _ := callParams.Arguments["query"].(string)
			topK := 5
			if kVal, ok := callParams.Arguments["top_k"].(float64); ok && kVal > 0 {
				topK = int(kVal)
			}

			titles, urls, bodies := indexer.GlobalEngine.GetMetadataMaps()
			bm25Hits := bm25.RankDocuments(
				query,
				indexer.GlobalEngine.Inverted,
				titles,
				urls,
				bodies,
				topK*2,
			)

			queryVec := vector.GenerateFeatureVector(query, 128)
			vecHits := embedder.GlobalVectorIndex.SearchNearest(queryVec, topK*2)

			fusedHits := hybrid.ReciprocalRankFusion(bm25Hits, vecHits, topK)

			var buf bytes.Buffer
			buf.WriteString(fmt.Sprintf("Hybrid Search Results for query: %q (Top %d)\n\n", query, len(fusedHits)))
			for i, hit := range fusedHits {
				buf.WriteString(fmt.Sprintf("[%d] %s\n", i+1, hit.Title))
				buf.WriteString(fmt.Sprintf("    URL: %s\n", hit.URL))
				buf.WriteString(fmt.Sprintf("    Score: %.6f (BM25 Rank: #%d, Vector Rank: #%d)\n", hit.RRFScore, hit.BM25Rank, hit.VectorRank))
				buf.WriteString(fmt.Sprintf("    Snippet: %s\n\n", hit.Snippet))
			}

			result = map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": buf.String(),
					},
				},
			}

		default:
			rpcErr = &RPCError{Code: -32601, Message: "Unknown tool name: " + callParams.Name}
		}

	case "resources/list":
		result = map[string]interface{}{
			"resources": []map[string]interface{}{
				{
					"uri":         "agentlimbs://stats",
					"name":        "Corpus Metrics",
					"description": "Corpus stats including total documents, vocabulary size, and average document length.",
					"mimeType":    "text/plain",
				},
			},
		}

	case "resources/read":
		var resParams struct {
			URI string `json:"uri"`
		}
		json.Unmarshal(req.Params, &resParams)

		if resParams.URI == "agentlimbs://stats" {
			totalDocs, avgLen, vocabSize := indexer.GlobalEngine.Inverted.GetStats()
			outText := fmt.Sprintf("AgentLimbs Corpus Stats:\n- Total Documents: %d\n- Average Doc Length: %.2f tokens\n- Vocabulary Size: %d terms\n",
				totalDocs, avgLen, vocabSize)

			result = map[string]interface{}{
				"contents": []map[string]interface{}{
					{
						"uri":      "agentlimbs://stats",
						"mimeType": "text/plain",
						"text":     outText,
					},
				},
			}
		} else {
			rpcErr = &RPCError{Code: -32602, Message: "Unknown resource URI: " + resParams.URI}
		}

	default:
		rpcErr = &RPCError{Code: -32601, Message: "Method not found: " + req.Method}
	}

	if rpcErr != nil {
		respMap := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"error":   rpcErr,
		}
		return json.Marshal(respMap)
	}

	respMap := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      req.ID,
		"result":  result,
	}
	return json.Marshal(respMap)
}
