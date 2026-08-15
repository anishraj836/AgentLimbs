package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJobManager_SyncAndAsyncCrawl(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		switch r.URL.Path {
		case "/":
			fmt.Fprintf(w, `<html><body><h1>Home</h1><a href="/page1">Page 1</a><a href="/page2">Page 2</a></body></html>`)
		case "/page1":
			fmt.Fprintf(w, `<html><body><h1>Page 1</h1><a href="/page3">Page 3</a></body></html>`)
		case "/page2":
			fmt.Fprintf(w, `<html><body><h1>Page 2 Content</h1></body></html>`)
		case "/page3":
			fmt.Fprintf(w, `<html><body><h1>Page 3 Deep Content</h1></body></html>`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewTestClient(true)
	jm := NewJobManager(client, "testdata")
	defer jm.Close()

	// 1. Test Synchronous Crawl
	jobSync, err := jm.StartCrawl(context.Background(), CrawlRequest{
		URL:           server.URL + "/",
		MaxDepth:      2,
		MaxPages:      10,
		Concurrency:   4,
		RateLimitRPS:  50.0,
		Async:         false,
		AllowLoopback: true,
	})
	if err != nil {
		t.Fatalf("Sync crawl failed: %v", err)
	}

	if jobSync.GetStatus() != "completed" {
		t.Errorf("expected status 'completed', got %q", jobSync.GetStatus())
	}
	if jobSync.PagesCrawled.Load() != 4 {
		t.Errorf("expected 4 pages crawled, got %d", jobSync.PagesCrawled.Load())
	}

	// 2. Test Asynchronous Crawl & Polling
	jobAsync, err := jm.StartCrawl(context.Background(), CrawlRequest{
		URL:           server.URL + "/",
		MaxDepth:      2,
		MaxPages:      10,
		Concurrency:   4,
		RateLimitRPS:  50.0,
		Async:         true,
		AllowLoopback: true,
	})
	if err != nil {
		t.Fatalf("Async crawl failed: %v", err)
	}

	// Poll until completed
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		polled, found := jm.GetJob(jobAsync.ID)
		if !found {
			t.Fatalf("job not found in manager: %s", jobAsync.ID)
		}
		if polled.GetStatus() == "completed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if jobAsync.GetStatus() != "completed" {
		t.Errorf("expected async job to complete, got %q", jobAsync.GetStatus())
	}
}

func TestJobManager_CancelJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond) // Slow response to allow inflight cancel
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><h1>Slow Page</h1></body></html>`))
	}))
	defer server.Close()

	client := NewTestClient(true)
	jm := NewJobManager(client, "testdata")
	defer jm.Close()

	job, err := jm.StartCrawl(context.Background(), CrawlRequest{
		URL:           server.URL + "/",
		MaxDepth:      2,
		MaxPages:      50,
		Concurrency:   2,
		Async:         true,
		AllowLoopback: true,
	})
	if err != nil {
		t.Fatalf("StartCrawl failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	cancelled := jm.CancelJob(job.ID)
	if !cancelled {
		t.Fatalf("expected CancelJob to return true")
	}

	if job.GetStatus() != "cancelled" {
		t.Errorf("expected status 'cancelled', got %q", job.GetStatus())
	}
}

func TestJobManager_ErrorIsolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body><a href="/404-page">Broken</a><a href="/good-page">Good</a></body></html>`))
		case "/404-page":
			http.NotFound(w, r)
		case "/good-page":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<html><body><h1>Good Page</h1></body></html>`))
		}
	}))
	defer server.Close()

	client := NewTestClient(true)
	jm := NewJobManager(client, "testdata")
	defer jm.Close()

	job, err := jm.StartCrawl(context.Background(), CrawlRequest{
		URL:           server.URL + "/",
		MaxDepth:      2,
		MaxPages:      10,
		Async:         false,
		AllowLoopback: true,
	})
	if err != nil {
		t.Fatalf("StartCrawl failed: %v", err)
	}

	if job.GetStatus() != "completed" {
		t.Errorf("expected crawl to complete despite 404 page, got %q", job.GetStatus())
	}
	if job.ErrorsCount.Load() == 0 {
		t.Errorf("expected at least 1 isolated error recorded")
	}
	if len(job.RecentErrors) == 0 || !strings.Contains(job.RecentErrors[0], "404") {
		t.Errorf("expected 404 error in RecentErrors, got: %v", job.RecentErrors)
	}
}

func TestJobManager_TTLEviction(t *testing.T) {
	jm := NewJobManager(NewTestClient(true), "testdata")
	defer jm.Close()
	jm.ttl = 10 * time.Millisecond // Short TTL for testing

	job := &CrawlJob{
		ID:        "job-to-evict",
		Status:    "completed",
		StartTime: time.Now().Add(-1 * time.Hour),
	}
	past := time.Now().Add(-1 * time.Hour)
	job.EndTime = &past

	jm.jobs.Store(job.ID, job)

	time.Sleep(20 * time.Millisecond)
	jm.EvictExpiredJobs()

	if _, found := jm.GetJob("job-to-evict"); found {
		t.Errorf("expected job to be evicted after TTL expiration")
	}
}
