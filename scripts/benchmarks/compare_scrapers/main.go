package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/crawler-monorepo/internal/extractor"
)

type BenchmarkResult struct {
	SiteName         string  `json:"site_name"`
	URL              string  `json:"url"`
	RawBytes         int     `json:"raw_bytes"`
	RawTokens        int     `json:"raw_tokens"`
	AgentLimbsBytes  int     `json:"agent_limbs_bytes"`
	AgentLimbsTokens int     `json:"agent_limbs_tokens"`
	AgentLimbsTimeMs float64 `json:"agent_limbs_time_ms"`
	PythonCoreBytes  int     `json:"python_core_bytes"`
	PythonCoreTokens int     `json:"python_core_tokens"`
	PythonCoreTimeMs float64 `json:"python_core_time_ms"`
	TokenSavingsPct  float64 `json:"token_savings_pct"`
}

func fetchRawHTML(targetURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func runPythonCore(htmlContent string) (markdown string, elapsedMs float64, err error) {
	script := `
import sys, time
from markdownify import markdownify as md

html_data = sys.stdin.read()
t0 = time.perf_counter()
# Standard Readability / Markdownify pipeline used by Python LLM scrapers
cleaned = md(html_data, strip=['script', 'style', 'nav', 'footer', 'noscript', 'svg', 'iframe'])
t1 = time.perf_counter()

print(f"TIME_MS:{(t1 - t0)*1000:.3f}")
print("---CONTENT---")
print(cleaned.strip())
`
	cmd := exec.Command("python3", "-c", script)
	cmd.Stdin = bytes.NewReader([]byte(htmlContent))
	var out bytes.Buffer
	var errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err = cmd.Run()
	if err != nil {
		return "", 0, fmt.Errorf("python err: %v | stderr: %s", err, errBuf.String())
	}

	parts := bytes.Split(out.Bytes(), []byte("---CONTENT---\n"))
	if len(parts) < 2 {
		return "", 0, fmt.Errorf("unexpected output")
	}

	header := string(parts[0])
	markdown = string(parts[1])
	fmt.Sscanf(header, "TIME_MS:%f", &elapsedMs)
	return markdown, elapsedMs, nil
}

func main() {
	urls := []struct {
		Name string
		URL  string
	}{
		{"Go Getting Started Tutorial", "https://go.dev/doc/tutorial/getting-started"},
		{"Docker Get Started Guide", "https://docs.docker.com/get-started/"},
		{"Wikipedia: Go Language", "https://en.wikipedia.org/wiki/Go_(programming_language)"},
		{"HttpBin Narrative Story", "https://httpbin.org/html"},
		{"Hacker News Front Page", "https://news.ycombinator.com"},
	}

	fmt.Println("==================================================================================")
	fmt.Println("   🔬 HEAD-TO-HEAD TOKEN EFFICIENCY BENCHMARK: AGENTLIMBS VS PYTHON SCRAPERS      ")
	fmt.Println("==================================================================================")
	fmt.Printf("Tokenizer: OpenAI cl100k_base (GPT-4 / Claude / DeepSeek)\n")
	fmt.Println("----------------------------------------------------------------------------------")

	var results []BenchmarkResult

	for idx, item := range urls {
		fmt.Printf("🌐 [%d/%d] Fetching live HTML: %s (%s)...\n", idx+1, len(urls), item.Name, item.URL)
		rawHTML, err := fetchRawHTML(item.URL)
		if err != nil {
			fmt.Printf("   ❌ Fetch failed: %v\n", err)
			continue
		}

		rawStr := string(rawHTML)
		rawBytes := len(rawHTML)
		rawTokens := extractor.CountBPETokens(rawStr)

		// 1. AgentLimbs DOM AST Parser (Pure Go)
		t0 := time.Now()
		agentLimbsMD, agentLimbsTokens, _ := extractor.ConvertHTMLToMarkdown(item.URL, rawHTML, "clean_rag")
		agentLimbsDuration := time.Since(t0)
		agentLimbsTimeMs := float64(agentLimbsDuration.Microseconds()) / 1000.0
		agentLimbsBytes := len(agentLimbsMD)

		// 2. Python Open-Source Scraper Pipeline (Markdownify / Readability)
		pyMD, pyTimeMs, pyErr := runPythonCore(rawStr)
		pyBytes := len(pyMD)
		pyTokens := 0
		if pyErr == nil {
			pyTokens = extractor.CountBPETokens(pyMD)
		} else {
			fmt.Printf("   ⚠️ Python error: %v\n", pyErr)
		}

		savingsPct := (1.0 - (float64(agentLimbsTokens) / float64(rawTokens))) * 100.0

		res := BenchmarkResult{
			SiteName:         item.Name,
			URL:              item.URL,
			RawBytes:         rawBytes,
			RawTokens:        rawTokens,
			AgentLimbsBytes:  agentLimbsBytes,
			AgentLimbsTokens: agentLimbsTokens,
			AgentLimbsTimeMs: agentLimbsTimeMs,
			PythonCoreBytes:  pyBytes,
			PythonCoreTokens: pyTokens,
			PythonCoreTimeMs: pyTimeMs,
			TokenSavingsPct:  savingsPct,
		}
		results = append(results, res)

		fmt.Printf("   • Raw HTML        : %8d bytes | %6d tokens\n", rawBytes, rawTokens)
		fmt.Printf("   • AgentLimbs (Go) : %8d bytes | %6d tokens | Time: %6.2f ms | 🔥 Savings: %5.1f%%\n",
			agentLimbsBytes, agentLimbsTokens, agentLimbsTimeMs, savingsPct)
		fmt.Printf("   • Python Scraper  : %8d bytes | %6d tokens | Time: %6.2f ms\n\n",
			pyBytes, pyTokens, pyTimeMs)
	}

	fmt.Println("==================================================================================")
	fmt.Println("                        FINAL TOKEN EFFICIENCY SUMMARY                            ")
	fmt.Println("==================================================================================")
	fmt.Printf("%-28s | %-12s | %-14s | %-14s | %-10s\n", "Page Name", "Raw Tokens", "AgentLimbs (Go)", "Python Scraper", "Savings %")
	fmt.Println("----------------------------------------------------------------------------------")

	var totRaw, totAL, totPy int
	var totALTime, totPyTime float64
	for _, r := range results {
		fmt.Printf("%-28s | %12d | %14d | %14d | %9.1f%%\n",
			r.SiteName, r.RawTokens, r.AgentLimbsTokens, r.PythonCoreTokens, r.TokenSavingsPct)
		totRaw += r.RawTokens
		totAL += r.AgentLimbsTokens
		totPy += r.PythonCoreTokens
		totALTime += r.AgentLimbsTimeMs
		totPyTime += r.PythonCoreTimeMs
	}
	fmt.Println("----------------------------------------------------------------------------------")
	totSavings := (1.0 - (float64(totAL) / float64(totRaw))) * 100.0
	fmt.Printf("%-28s | %12d | %14d | %14d | %9.1f%%\n",
		"TOTAL (ALL 5 SITES)", totRaw, totAL, totPy, totSavings)
	fmt.Println("==================================================================================")
	fmt.Printf("⚡ Average Speed Comparison: AgentLimbs = %.2f ms/page | Python Scrapers = %.2f ms/page (AgentLimbs is %.1fx faster)\n",
		totALTime/float64(len(results)), totPyTime/float64(len(results)), (totPyTime/totALTime))
	fmt.Println("==================================================================================")

	data, _ := json.MarshalIndent(results, "", "  ")
	_ = os.WriteFile("compare_results.json", data, 0644)
}
