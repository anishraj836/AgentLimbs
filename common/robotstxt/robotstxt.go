package robotstxt

import (
	"bufio"
	"strings"
	"sync"
	"time"
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

func (rd *RobotsData) IsAllowed(userAgent, path string) bool {
	rd.mu.RLock()
	defer rd.mu.RUnlock()

	uaLower := strings.ToLower(userAgent)

	for _, group := range rd.groups {
		if group.UserAgent == "*" || strings.Contains(uaLower, group.UserAgent) {
			for _, disallow := range group.Disallowed {
				if disallow != "" && strings.HasPrefix(path, disallow) {
					return false
				}
			}
		}
	}

	return true
}

type DomainCacheManager struct {
	mu     sync.RWMutex
	cache  map[string]*RobotsData
	expiry map[string]time.Time
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

// IsDomainAllowed checks if a path is allowed for a user agent under a cached domain's rules.
func (cm *DomainCacheManager) IsDomainAllowed(userAgent, domain, path string) bool {
	cm.mu.RLock()
	data, exists := cm.cache[domain]
	exp, _ := cm.expiry[domain]
	cm.mu.RUnlock()

	if !exists || time.Now().After(exp) {
		return true // Fail-open default policy if unparsed or expired
	}
	return data.IsAllowed(userAgent, path)
}

var GlobalRobotsCache = &RobotsData{groups: make([]RobotsGroup, 0)}

// IsAllowed is a package-level helper for Robots.txt compliance check.
func IsAllowed(userAgent, targetURL string) bool {
	return GlobalRobotsCache.IsAllowed(userAgent, targetURL)
}
