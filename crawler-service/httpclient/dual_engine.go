package httpclient

import (
	"github.com/crawler-monorepo/internal/crawler"
)

func DetectJSShell(htmlBytes []byte) bool {
	return crawler.DetectJSShell(htmlBytes)
}
