package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

var (
	PagesCrawled = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crawler_pages_crawled_total",
		Help: "The total number of pages crawled",
	})
	CrawlErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "crawler_errors_total",
		Help: "The total number of errors encountered during crawling",
	}, []string{"type"})
	HttpLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "crawler_http_latency_seconds",
		Help:    "Latency of HTTP requests made by the crawler",
		Buckets: prometheus.DefBuckets,
	}, []string{"domain"})
	URLsDiscovered = promauto.NewCounter(prometheus.CounterOpts{
		Name: "crawler_urls_discovered_total",
		Help: "The total number of URLs discovered by the parser",
	})
)

// InitMetricsServer starts the prometheus metrics server on a dedicated ServeMux
// to avoid conflicts with the application's DefaultServeMux or chi router.
func InitMetricsServer(port string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	go func() {
		if err := http.ListenAndServe(":"+port, mux); err != nil {
			panic(err)
		}
	}()
}
