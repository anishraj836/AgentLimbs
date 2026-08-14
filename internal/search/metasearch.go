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

type DDGResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type MetasearchAdapter struct {
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
		baseURL:          "https://html.duckduckgo.com/html/",
		httpClient:       &http.Client{Timeout: 5 * time.Second},
		crawlerClient:    crawler.NewClient(),
		engine:           engine,
		timeout:          1500 * time.Millisecond,
		concurrencyLimit: 10,
	}
}

func (a *MetasearchAdapter) WithBaseURL(baseURL string) *MetasearchAdapter {
	a.baseURL = baseURL
	return a
}

func (a *MetasearchAdapter) WithHTTPClient(client *http.Client) *MetasearchAdapter {
	a.httpClient = client
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

	sfKey := strings.ToLower(strings.TrimSpace(query))

	val, err, _ := a.singleflightGroup.Do(sfKey, func() (interface{}, error) {
		execCtx, cancel := context.WithTimeout(context.Background(), searchDeadline)
		defer cancel()
		return a.executeMetasearch(execCtx, query, topK)
	})

	if err != nil {
		return nil, err
	}
	if val == nil {
		return []HybridSearchHit{}, nil
	}
	return val.([]HybridSearchHit), nil
}

func (a *MetasearchAdapter) executeMetasearch(ctx context.Context, query string, topK int) ([]HybridSearchHit, error) {
	ddgResults, _ := a.QueryDuckDuckGo(ctx, query)

	if len(ddgResults) > 0 {
		var g errgroup.Group
		limit := a.concurrencyLimit
		if limit <= 0 {
			limit = 10
		}
		g.SetLimit(limit)

		for _, res := range ddgResults {
			targetURL := res.URL
			initialTitle := res.Title

			g.Go(func() error {
				if ctx.Err() != nil {
					return nil
				}

				var fetchRes *crawler.FetchResult
				var err error
				if a.crawlerClient != nil {
					fetchRes, err = a.crawlerClient.FetchSmart(ctx, targetURL, false)
				} else {
					req, reqErr := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
					if reqErr != nil {
						return nil
					}
					resp, doErr := a.httpClient.Do(req)
					if doErr != nil {
						return nil
					}
					fetchRes = &crawler.FetchResult{Response: resp, FinalURL: targetURL}
				}

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
	bm25Hits := a.engine.Inverted.RankDocuments(query, titles, urls, bodies, topK*2)
	vectorHits := a.engine.SearchVector(query, topK*2)

	hits := ReciprocalRankFusion(query, bm25Hits, vectorHits, topK, titles, urls, bodies)
	return hits, nil
}

func (a *MetasearchAdapter) QueryDuckDuckGo(ctx context.Context, query string) ([]DDGResult, error) {
	reqURL := a.baseURL
	if strings.Contains(reqURL, "?") {
		reqURL += "&q=" + url.QueryEscape(query)
	} else {
		reqURL += "?q=" + url.QueryEscape(query)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	profile := crawler.GetRotatedHeaderProfile()
	crawler.ApplyAntiBotHeaders(req, profile)

	client := a.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024))
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DuckDuckGo endpoint returned HTTP status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, 10*1024*1024)
	htmlBytes, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}

	return ParseDDGHTML(htmlBytes)
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
