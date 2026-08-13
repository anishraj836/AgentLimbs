package main

import (
	"fmt"
	"github.com/pkoukk/tiktoken-go"
	"github.com/crawler-monorepo/internal/extractor"
)

func main() {
	tke, err := tiktoken.GetEncoding("cl100k_base")
	if err != nil {
		fmt.Printf("Failed to load tiktoken encoding: %v\n", err)
		return
	}

	samples := map[string]string{
		"HTML Spec / Doc Page": `<!DOCTYPE html><html><head><title>Documentation</title><style>body { font-family: sans-serif; }</style><script>console.log("analytics");</script></head><body><header><nav><a href="/home">Home</a><a href="/docs">Docs</a></nav></header><main><h1>Go Concurrency & Memory Management</h1><p>Go goroutines provide lightweight concurrency. Unlike OS threads which allocate 1MB stack memory, Go goroutines start with a 2KB stack that grows dynamically.</p><p>Memory allocation is managed by a garbage collector using a tri-color mark-and-sweep algorithm.</p><table><tr><th>Metric</th><th>OS Thread</th><th>Goroutine</th></tr><tr><td>Stack Size</td><td>1 MB</td><td>2 KB</td></tr><tr><td>Switch Overhead</td><td>~1 ms</td><td>~100 ns</td></tr></table></main><footer><p>&copy; 2026 Docs Corp. All rights reserved.</p></footer></body></html>`,
		"News / Article Page":  `<!DOCTYPE html><html><head><title>Breaking News</title><script type="text/javascript" src="ads.js"></script></head><body><div class="ad-banner"><iframe src="ad.html"></iframe></div><article><h1>New Breakthrough in Quantum Computing</h1><p class="byline">By Tech Reporter | Published August 2026</p><p>Researchers at the AI Laboratory have demonstrated a fault-tolerant topological qubit system achieving 99.99% fidelity under room temperature conditions.</p><p>This achievement accelerates commercial quantum error correction timelines by five years.</p></article><aside><div class="related"><h3>Sponsored Links</h3><ul><li>Buy Cheap VPN</li><li>Best Credit Cards</li></ul></div></aside></body></html>`,
		"E-Commerce Product Page": `<!DOCTYPE html><html><head><title>UltraBook Pro 15</title></head><body><div class="header">Navigation Menu</div><div class="product"><h1 class="title">UltraBook Pro 15-inch Laptop</h1><div class="price">$1,499.00</div><div class="description"><p>Features 16-core CPU, 32GB Unified Memory, 1TB NVMe SSD, and 18-hour battery life. Designed for machine learning engineering and high-throughput data processing.</p></div></div><div class="comments">User Comments & Ads...</div></body></html>`,
	}

	fmt.Println("=========================================================================")
	fmt.Println("             AGENTLIMBS AST DOM TOKEN SAVINGS BENCHMARK                  ")
	fmt.Println("=========================================================================")

	var totalRawTokens, totalCleanTokens int

	for name, rawHTML := range samples {
		rawTokenList := tke.Encode(rawHTML, nil, nil)
		rawTokenCount := len(rawTokenList)

		cleanMD, _, _ := extractor.ConvertHTMLToMarkdown("https://example.com/test", []byte(rawHTML), "clean_rag")
		cleanTokenList := tke.Encode(cleanMD, nil, nil)
		cleanTokenCount := len(cleanTokenList)

		savings := (1.0 - float64(cleanTokenCount)/float64(rawTokenCount)) * 100.0

		totalRawTokens += rawTokenCount
		totalCleanTokens += cleanTokenCount

		fmt.Printf("[%s]\n", name)
		fmt.Printf("  • Raw HTML Tokens:       %d tokens (%d bytes)\n", rawTokenCount, len(rawHTML))
		fmt.Printf("  • Clean RAG MD Tokens:  %d tokens (%d bytes)\n", cleanTokenCount, len(cleanMD))
		fmt.Printf("  • BPE Token Savings:    %.2f%%\n\n", savings)
	}

	avgSavings := (1.0 - float64(totalCleanTokens)/float64(totalRawTokens)) * 100.0
	fmt.Println("-------------------------------------------------------------------------")
	fmt.Printf("OVERALL AVERAGE TOKEN SAVINGS: %.2f%%\n", avgSavings)
	fmt.Println("=========================================================================")
}
