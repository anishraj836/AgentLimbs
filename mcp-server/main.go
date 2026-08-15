package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/crawler-monorepo/common/config"
	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/redis"
	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/mcp"
	"github.com/crawler-monorepo/internal/storage"
)

const maxLineSize = 10 * 1024 * 1024

var errLineTooLong = errors.New("line length exceeded 10MB")

// readBoundedLine reads a line up to maxBytes from reader using ReadLine.
// If the line exceeds maxBytes without a newline, it discards remaining bytes until newline or EOF,
// and returns errLineTooLong.
func readBoundedLine(reader *bufio.Reader, maxBytes int) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := reader.ReadLine()
		if err != nil {
			if len(line) == 0 && len(chunk) == 0 {
				return nil, err
			}
			if len(line)+len(chunk) > maxBytes {
				return nil, errLineTooLong
			}
			line = append(line, chunk...)
			return line, nil
		}
		if len(line)+len(chunk) > maxBytes {
			for isPrefix && err == nil {
				_, isPrefix, err = reader.ReadLine()
			}
			return nil, errLineTooLong
		}
		line = append(line, chunk...)
		if !isPrefix {
			break
		}
	}
	return line, nil
}

func main() {
	cfg, err := config.LoadAndValidate()
	if err != nil {
		fmt.Fprintf(os.Stderr, "MCP Server Config Error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	storage.InitDB(cfg.DatabaseURL)
	defer storage.CloseDB()
	defer redis.CloseRedis()

	if err := index.GlobalEngine.LoadFromDB(ctx); err != nil {
		logger.Log.Info("No existing persisted corpus loaded from DB into MCP server memory")
	}

	client := crawler.NewClient()
	reader := bufio.NewReaderSize(os.Stdin, 64*1024)

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info("MCP Server shutting down gracefully")
			return
		default:
		}

		line, err := readBoundedLine(reader, maxLineSize)
		if err != nil {
			if errors.Is(err, errLineTooLong) {
				logger.Log.Error("MCP Stdio Read error: line length exceeded 10MB")
				errResp := map[string]interface{}{
					"jsonrpc": "2.0",
					"id":      nil,
					"error": map[string]interface{}{
						"code":    -32700,
						"message": "Parse error: line length exceeded 10MB",
					},
				}
				if respBytes, marshalErr := json.Marshal(errResp); marshalErr == nil {
					fmt.Println(string(respBytes))
				}
				continue
			}
			if errors.Is(err, io.EOF) || err == io.EOF {
				break
			}
			logger.Log.Error("MCP Stdio Read error: " + err.Error())
			break
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}

		var rawMessage json.RawMessage
		if unmarshalErr := json.Unmarshal(trimmed, &rawMessage); unmarshalErr != nil {
			logger.Log.Error("MCP Stdio Decode error: " + unmarshalErr.Error())
			errResp := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      nil,
				"error": map[string]interface{}{
					"code":    -32700,
					"message": "Parse error: " + unmarshalErr.Error(),
				},
			}
			if respBytes, marshalErr := json.Marshal(errResp); marshalErr == nil {
				fmt.Println(string(respBytes))
			}
			continue
		}

		if len(rawMessage) == 0 {
			continue
		}

		respBytes, rpcErr := mcp.HandleRPCMessage(rawMessage, client)
		if rpcErr != nil {
			logger.Log.Error("MCP RPC processing error: " + rpcErr.Error())
			continue
		}

		if len(respBytes) > 0 {
			fmt.Println(string(respBytes))
		}
	}
}
