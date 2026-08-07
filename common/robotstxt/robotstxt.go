package robotstxt

import (
	"github.com/crawler-monorepo/internal/crawler"
)

type RobotsGroup = crawler.RobotsGroup
type RobotsData = crawler.RobotsData
type DomainCacheManager = crawler.DomainCacheManager

var GlobalDomainCache = crawler.GlobalDomainCache

func ParseRobotsTxt(content string) *RobotsData {
	return crawler.ParseRobotsTxt(content)
}

func EnsureRobotsCached(domain string, fetchFunc func(domain string) (string, error)) (*RobotsData, error) {
	return crawler.GlobalDomainCache.EnsureRobotsCached(domain, fetchFunc)
}

func IsAllowed(userAgent, targetURL string) bool {
	return crawler.IsAllowed(userAgent, targetURL)
}
