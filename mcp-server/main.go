package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/crawler-monorepo/common/logger"
	"github.com/crawler-monorepo/common/mcp"
	"github.com/crawler-monorepo/crawler-service/httpclient"
)

func main() {
	logger.InitLogger("production")
	defer logger.Sync()

	client := httpclient.NewClient()
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

		fmt.Println(string(respBytes))
	}

	if err := scanner.Err(); err != nil {
		logger.Log.Error("MCP Stdio Scanner error: " + err.Error())
	}
}
