package main

import (
	"bufio"
	"context"
	"fmt"
	"os"

	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/index"
	"github.com/crawler-monorepo/internal/mcp"
	"github.com/crawler-monorepo/internal/storage"
)

func main() {
	storage.InitDB(os.Getenv("DATABASE_URL"))
	if err := index.GlobalEngine.LoadFromDB(context.Background()); err != nil {
		logger.Log.Info("No existing persisted corpus loaded from DB into MCP server memory")
	}

	client := crawler.NewClient()
	scanner := bufio.NewScanner(os.Stdin)

	// Increase buffer limit for large JSON-RPC messages if needed
	buf := make([]byte, 10*1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		respBytes, err := mcp.HandleRPCMessage(line, client)
		if err != nil {
			logger.Log.Error("MCP RPC processing error: " + err.Error())
			continue
		}

		if len(respBytes) > 0 {
			fmt.Println(string(respBytes))
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Log.Error("MCP Stdio Scanner error: " + err.Error())
	}
}
