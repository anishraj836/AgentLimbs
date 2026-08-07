package robotstxt

import (
	"bufio"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type RobotsGroup struct {
	UserAgent  string
	Disallowed []string
	CrawlDelay int
}

type RobotsData struct {
	mu     sync.RWMutex
	groups []RobotsGroup
}

func ParseRobotsTxt(content string) *RobotsData {
	rd := &RobotsData{groups: make([]RobotsGroup, 0)}
	scanner := bufio.NewScanner(strings.NewReader(content))

	var currentGroup *RobotsGroup

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Remove comments
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			if currentGroup != nil {
				rd.groups = append(rd.groups, *currentGroup)
			}
			currentGroup = &RobotsGroup{
				UserAgent:  strings.ToLower(val),
				Disallowed: make([]string, 0),
			}
		case "disallow":
			if currentGroup != nil && val != "" {
				currentGroup.Disallowed = append(currentGroup.Disallowed, val)
			}
		}
	}

	if currentGroup != nil {
		rd.groups = append(rd.groups, *currentGroup)
	}

	return rd
}

func (rd *RobotsData) IsAllowed(userAgent, targetURL string) bool {
	rd.mu.RLock()
	defer rd.mu.RUnlock()

	reqURL, err := url.Parse(targetURL)
	reqPath := targetURL
	if err == nil {
		reqPath = reqURL.Path
	}
	if reqPath == "" {
		reqPath = "/"
	}

	uaLower := strings.ToLower(userAgent)

	for _, group := range rd.groups {
		if group.UserAgent == "*" || strings.Contains(uaLower, group.UserAgent) {
			for _, disallow := range group.Disallowed {
				if disallow != "" && strings.HasPrefix(reqPath, disallow) {
					return false
				}
			}
		}
	}

	return true
}

type DomainCacheManager struct {
	mu      sync.RWMutex
	cache   map[string]*RobotsData
	expiry  map[string]time.Time
	sfGroup singleflight.Group
}

var GlobalDomainCache = &DomainCacheManager{
	cache:  make(map[string]*RobotsData),
	expiry: make(map[string]time.Time),
}

// FetchAndCache parses and stores a domain's robots.txt rules with a 24-hour TTL cache.
func (cm *DomainCacheManager) FetchAndCache(domain, rawContent string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.cache[domain] = ParseRobotsTxt(rawContent)
	cm.expiry[domain] = time.Now().Add(24 * time.Hour)
}

// EnsureRobotsCached fetches and caches robots.txt for a domain if not already cached/valid,
// using singleflight.Group to coalesce concurrent requests for the same uncached domain.
func (cm *DomainCacheManager) EnsureRobotsCached(domain string, fetchFunc func(domain string) (string, error)) (*RobotsData, error) {
	cm.mu.RLock()
	data, exists := cm.cache[domain]
	exp, _ := cm.expiry[domain]
	cm.mu.RUnlock()

	if exists && time.Now().Before(exp) {
		return data, nil
	}

	val, err, _ := cm.sfGroup.Do(domain, func() (interface{}, error) {
		cm.mu.RLock()
		data, exists := cm.cache[domain]
		exp, _ := cm.expiry[domain]
		cm.mu.RUnlock()

		if exists && time.Now().Before(exp) {
			return data, nil
		}

		rawContent, err := fetchFunc(domain)
		if err != nil {
			return nil, err
		}

		cm.FetchAndCache(domain, rawContent)

		cm.mu.RLock()
		data = cm.cache[domain]
		cm.mu.RUnlock()

		return data, nil
	})

	if err != nil {
		return nil, err
	}

	return val.(*RobotsData), nil
}

// EnsureRobotsCached is a package-level helper that ensures robots.txt is cached for a domain.
func EnsureRobotsCached(domain string, fetchFunc func(domain string) (string, error)) (*RobotsData, error) {
	return GlobalDomainCache.EnsureRobotsCached(domain, fetchFunc)
}

// HasDomainCached checks if a valid unexpired robots.txt rule is present in cache.
func (cm *DomainCacheManager) HasDomainCached(targetURL string) bool {
	reqURL, err := url.Parse(targetURL)
	if err != nil || reqURL.Hostname() == "" {
		return true
	}
	domain := reqURL.Hostname()

	cm.mu.RLock()
	defer cm.mu.RUnlock()
	exp, exists := cm.expiry[domain]
	return exists && time.Now().Before(exp)
}

// IsDomainAllowed checks if a target URL path is allowed for a user agent under a cached domain's rules.
func (cm *DomainCacheManager) IsDomainAllowed(userAgent, targetURL string) (allowed bool, cached bool) {
	reqURL, err := url.Parse(targetURL)
	if err != nil {
		return true, false
	}
	domain := reqURL.Hostname()
	if domain == "" {
		return true, false
	}

	cm.mu.RLock()
	data, exists := cm.cache[domain]
	exp, _ := cm.expiry[domain]
	cm.mu.RUnlock()

	if !exists || time.Now().Before(exp) == false {
		return true, false // Uncached or expired
	}
	return data.IsAllowed(userAgent, targetURL), true
}

// IsAllowed is a package-level helper checking domain cache rules.
func IsAllowed(userAgent, targetURL string) bool {
	allowed, cached := GlobalDomainCache.IsDomainAllowed(userAgent, targetURL)
	if cached {
		return allowed
	}
	return true // Fail-open default policy
}
