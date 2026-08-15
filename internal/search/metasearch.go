package search

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/crawler-monorepo/common/stemmer"
	"github.com/crawler-monorepo/common/stopwords"
	"github.com/crawler-monorepo/internal/crawler"
	"github.com/crawler-monorepo/internal/extractor"
	"github.com/crawler-monorepo/internal/index"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

type DDGResult = SearchResult

type MetasearchAdapter struct {
	provider          SearchProvider
	baseURL           string
	httpClient        *http.Client
	crawlerClient     *crawler.Client
	engine            *index.Engine
	singleflightGroup singleflight.Group
	timeout           time.Duration
	concurrencyLimit  int
}

func NewMetasearchAdapter(engine *index.Engine) *MetasearchAdapter {
	if engine == nil {
		engine = index.GlobalEngine
	}
	return &MetasearchAdapter{
		provider:         NewDuckDuckGoSearchProvider(),
		baseURL:          "https://html.duckduckgo.com/html/",
		httpClient:       &http.Client{Timeout: 5 * time.Second},
		crawlerClient:    crawler.NewClient(),
		engine:           engine,
		timeout:          1500 * time.Millisecond,
		concurrencyLimit: 10,
	}
}

func (a *MetasearchAdapter) WithProvider(provider SearchProvider) *MetasearchAdapter {
	a.provider = provider
	return a
}

func (a *MetasearchAdapter) WithBaseURL(baseURL string) *MetasearchAdapter {
	a.baseURL = baseURL
	if ddg, ok := a.provider.(*DuckDuckGoSearchProvider); ok && ddg != nil {
		ddg.WithBaseURL(baseURL)
	}
	return a
}

func (a *MetasearchAdapter) WithHTTPClient(client *http.Client) *MetasearchAdapter {
	a.httpClient = client
	if ddg, ok := a.provider.(*DuckDuckGoSearchProvider); ok && ddg != nil {
		ddg.WithHTTPClient(client)
	}
	return a
}

func (a *MetasearchAdapter) WithCrawlerClient(client *crawler.Client) *MetasearchAdapter {
	a.crawlerClient = client
	return a
}

func (a *MetasearchAdapter) WithTimeout(timeout time.Duration) *MetasearchAdapter {
	a.timeout = timeout
	return a
}

func (a *MetasearchAdapter) WithConcurrencyLimit(limit int) *MetasearchAdapter {
	a.concurrencyLimit = limit
	return a
}

func (a *MetasearchAdapter) getProvider() SearchProvider {
	if a.provider != nil {
		return a.provider
	}
	ddg := NewDuckDuckGoSearchProvider()
	if a.baseURL != "" {
		ddg.WithBaseURL(a.baseURL)
	}
	if a.httpClient != nil {
		ddg.WithHTTPClient(a.httpClient)
	}
	return ddg
}

func (a *MetasearchAdapter) Search(ctx context.Context, query string, topK int) ([]HybridSearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []HybridSearchHit{}, nil
	}
	if topK <= 0 {
		topK = 5
	}

	searchDeadline := a.timeout
	if searchDeadline <= 0 {
		searchDeadline = 1500 * time.Millisecond
	}

	provider := a.getProvider()
	sfKey := fmt.Sprintf("%s:%s:%d", provider.Name(), strings.ToLower(strings.TrimSpace(query)), topK)

	ch := a.singleflightGroup.DoChan(sfKey, func() (interface{}, error) {
		execCtx, cancel := context.WithTimeout(context.Background(), searchDeadline)
		defer cancel()
		return a.executeMetasearch(execCtx, query, topK)
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		if res.Val == nil {
			return []HybridSearchHit{}, nil
		}
		return res.Val.([]HybridSearchHit), nil
	}
}

func (a *MetasearchAdapter) executeMetasearch(ctx context.Context, query string, topK int) ([]HybridSearchHit, error) {
	provider := a.getProvider()
	results, _ := provider.Search(ctx, query, topK*2)

	if len(results) > 0 {
		var g errgroup.Group
		limit := a.concurrencyLimit
		if limit <= 0 {
			limit = 10
		}
		g.SetLimit(limit)

		cClient := a.crawlerClient
		if cClient == nil {
			cClient = crawler.NewClient()
		}

		for _, res := range results {
			targetURL := res.URL
			initialTitle := res.Title

			g.Go(func() error {
				if ctx.Err() != nil {
					return nil
				}

				fetchRes, err := cClient.FetchSmart(ctx, targetURL, false)
				if err != nil || fetchRes == nil || fetchRes.Response == nil {
					return nil
				}
				defer fetchRes.Response.Body.Close()

				limitedBody := io.LimitReader(fetchRes.Response.Body, 10*1024*1024)
				bodyBytes, readErr := io.ReadAll(limitedBody)
				if readErr != nil {
					return nil
				}

				markdownText, tokenEst, title := extractor.ConvertHTMLToMarkdown(targetURL, bodyBytes, "clean_rag")
				if title == "" || title == targetURL {
					if initialTitle != "" {
						title = initialTitle
					}
				}

				cleanBody := markdownText
				rawTokens := strings.Fields(strings.ToLower(cleanBody))
				termPositions := make(map[string][]int)
				for idx, raw := range rawTokens {
					clean := strings.Trim(raw, ".,!?:;\"'()[]{}")
					if clean == "" || stopwords.IsStopword(clean) {
						continue
					}
					stemmed := stemmer.Stem(clean)
					termPositions[stemmed] = append(termPositions[stemmed], idx)
				}

				a.engine.IndexDocumentWithSource(
					targetURL,
					title,
					cleanBody,
					termPositions,
					tokenEst,
					"metasearch",
					targetURL,
				)

				return nil
			})
		}

		_ = g.Wait()
	}

	titles, urls, bodies := a.engine.GetMetadataMaps()
	bm25Hits := a.engine.SearchBM25(query, topK*2)
	vectorHits := a.engine.SearchVector(query, topK*2)

	hits := ReciprocalRankFusion(query, bm25Hits, vectorHits, topK, titles, urls, bodies)
	return hits, nil
}

func (a *MetasearchAdapter) QueryDuckDuckGo(ctx context.Context, query string) ([]SearchResult, error) {
	ddg := NewDuckDuckGoSearchProvider()
	if a.baseURL != "" {
		ddg.WithBaseURL(a.baseURL)
	}
	if a.httpClient != nil {
		ddg.WithHTTPClient(a.httpClient)
	}
	return ddg.Search(ctx, query, 0)
}

func ParseDDGHTML(htmlContent []byte) ([]DDGResult, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(htmlContent))
	if err != nil {
		return nil, err
	}

	var results []DDGResult
	seenURLs := make(map[string]bool)

	doc.Find(".result, .web-result, .links_main, .result__body").Each(func(i int, s *goquery.Selection) {
		linkTag := s.Find("a.result__a, a.result__url, h2 a").First()
		if linkTag.Length() == 0 {
			return
		}

		rawHref, _ := linkTag.Attr("href")
		title := strings.TrimSpace(linkTag.Text())

		snippetTag := s.Find(".result__snippet, .links_snippet, a.result__snippet").First()
		snippet := strings.TrimSpace(snippetTag.Text())

		targetURL := ExtractDDGTargetURL(rawHref)
		if targetURL != "" && !crawler.IsMediaResourceURL(targetURL) && !seenURLs[targetURL] {
			seenURLs[targetURL] = true
			results = append(results, DDGResult{
				Title:   title,
				URL:     targetURL,
				Snippet: snippet,
			})
		}
	})

	if len(results) == 0 {
		doc.Find("a[href]").Each(func(i int, s *goquery.Selection) {
			rawHref, _ := s.Attr("href")
			targetURL := ExtractDDGTargetURL(rawHref)
			if targetURL != "" && !crawler.IsMediaResourceURL(targetURL) && !seenURLs[targetURL] {
				title := strings.TrimSpace(s.Text())
				if title == "" {
					title = targetURL
				}
				seenURLs[targetURL] = true
				results = append(results, DDGResult{
					Title: title,
					URL:   targetURL,
				})
			}
		})
	}

	return results, nil
}

func ExtractDDGTargetURL(rawHref string) string {
	rawHref = strings.TrimSpace(rawHref)
	if rawHref == "" {
		return ""
	}

	if strings.HasPrefix(rawHref, "//") {
		rawHref = "https:" + rawHref
	} else if strings.HasPrefix(rawHref, "/") {
		rawHref = "https://html.duckduckgo.com" + rawHref
	}

	parsed, err := url.Parse(rawHref)
	if err != nil {
		return ""
	}

	if uddg := parsed.Query().Get("uddg"); uddg != "" {
		target := uddg
		if unescaped, err := url.QueryUnescape(uddg); err == nil && unescaped != "" {
			target = unescaped
		}
		targetParsed, err := url.Parse(target)
		if err != nil {
			return ""
		}
		if targetParsed.Scheme != "http" && targetParsed.Scheme != "https" {
			return ""
		}
		if strings.Contains(strings.ToLower(targetParsed.Host), "duckduckgo.com") {
			return ""
		}
		return target
	}

	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		if strings.Contains(strings.ToLower(parsed.Host), "duckduckgo.com") {
			return ""
		}
		return rawHref
	}

	return ""
}
