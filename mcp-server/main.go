package main

import (
	"context"
	"encoding/json"
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
	decoder := json.NewDecoder(os.Stdin)

	for {
		select {
		case <-ctx.Done():
			logger.Log.Info("MCP Server shutting down gracefully")
			return
		default:
		}

		var rawMessage json.RawMessage
		if err := decoder.Decode(&rawMessage); err != nil {
			if err == io.EOF {
				break
			}
			logger.Log.Error("MCP Stdio Decode error: " + err.Error())
			break
		}

		if len(rawMessage) == 0 {
			continue
		}

		respBytes, err := mcp.HandleRPCMessage(rawMessage, client)
		if err != nil {
			logger.Log.Error("MCP RPC processing error: " + err.Error())
			continue
		}

		if len(respBytes) > 0 {
			fmt.Println(string(respBytes))
		}
	}
}
