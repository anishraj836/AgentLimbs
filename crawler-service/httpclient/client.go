package httpclient

import (
	"net/http"

	"github.com/crawler-monorepo/internal/crawler"
)

type Client = crawler.Client
type FetchResult = crawler.FetchResult
type HeaderProfile = crawler.HeaderProfile

var Chrome122Profiles = crawler.Chrome122Profiles

func NewClient() *Client {
	return crawler.NewClient()
}

func NewClientWithTransport(tr http.RoundTripper) *Client {
	return crawler.NewClientWithTransport(tr)
}

func NewTestClient(allowLoopback bool) *Client {
	return crawler.NewTestClient(allowLoopback)
}

func NewTestClientWithTransport(tr http.RoundTripper, allowLoopback bool) *Client {
	return crawler.NewTestClientWithTransport(tr, allowLoopback)
}

func GetRotatedHeaderProfile() HeaderProfile {
	return crawler.GetRotatedHeaderProfile()
}

func ApplyAntiBotHeaders(req *http.Request, profile HeaderProfile) {
	crawler.ApplyAntiBotHeaders(req, profile)
}

func IsSPAPlaceholder(html string) bool {
	return crawler.IsSPAPlaceholder(html)
}
