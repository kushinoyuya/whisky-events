package crawl

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/kushinoyuya/whisky-events/internal/extract"
)

// Fetcher downloads a page. *fetch.Client satisfies it; tests use a map.
type Fetcher interface {
	Get(ctx context.Context, url string) (string, error)
}

// Stats summarises one crawl run.
type Stats struct {
	Fetched   int // pages downloaded
	Extracted int // pages run through the extractors
	Cached    int // pages unchanged since last run (extraction skipped)
	Skipped   int // pages without whisky keywords
	Blocked   int // URLs disallowed by robots.txt
	Errors    int // fetch or extraction failures (crawl continues)
}

func (s Stats) String() string {
	return fmt.Sprintf("fetched %d, extracted %d, cached %d, skipped %d, robots-blocked %d, errors %d",
		s.Fetched, s.Extracted, s.Cached, s.Skipped, s.Blocked, s.Errors)
}

// Crawler visits sites one request at a time with a per-host delay.
type Crawler struct {
	Fetch     Fetcher
	Pipeline  *extract.Pipeline
	Cache     *Cache
	UserAgent string
	Delay     time.Duration // minimum gap between requests to the same host
	Log       io.Writer

	Now   func() time.Time    // injectable for tests
	Sleep func(time.Duration) // injectable for tests

	robots   map[string]*robots
	lastHit  map[string]time.Time
	stats    Stats
	visiting map[string]bool
}

// Run crawls every enabled site and returns the upcoming events found,
// deduplicated across pages. Errors on individual pages are logged and
// counted, not returned, so one broken site doesn't stop the crawl.
func (c *Crawler) Run(ctx context.Context, sites []Site) ([]extract.Event, Stats) {
	c.init()
	var all []extract.Event
	for _, s := range sites {
		if !s.Enabled {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		c.logf("site %s", s.Name)
		page, events := c.visit(ctx, s.URL)
		all = append(all, events...)

		if s.follow != nil && page != "" {
			links := findLinks(page, s.URL, s.follow, s.MaxLinks)
			c.logf("  %d link(s) to follow", len(links))
			for _, link := range links {
				if ctx.Err() != nil {
					break
				}
				_, events := c.visit(ctx, link)
				all = append(all, events...)
			}
		}
	}
	today := c.Now()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return extract.Dedupe(extract.Upcoming(all, today)), c.stats
}

func (c *Crawler) init() {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Sleep == nil {
		c.Sleep = time.Sleep
	}
	if c.Log == nil {
		c.Log = ioutil.Discard
	}
	c.robots = map[string]*robots{}
	c.lastHit = map[string]time.Time{}
	c.visiting = map[string]bool{}
	c.stats = Stats{}
}

// visit fetches one URL and returns its HTML (for link discovery) and events.
func (c *Crawler) visit(ctx context.Context, rawURL string) (string, []extract.Event) {
	if c.visiting[rawURL] {
		return "", nil
	}
	c.visiting[rawURL] = true

	u, err := url.Parse(rawURL)
	if err != nil {
		c.fail(rawURL, err)
		return "", nil
	}
	if !c.robotsFor(ctx, u).allowed(u.RequestURI()) {
		c.stats.Blocked++
		c.logf("  robots.txt disallows %s", rawURL)
		return "", nil
	}

	page, err := c.get(ctx, u.Host, rawURL)
	if err != nil {
		c.fail(rawURL, err)
		return "", nil
	}
	c.stats.Fetched++

	hash := contentHash(page)
	if prev, ok := c.Cache.Pages[rawURL]; ok && prev.Hash == hash {
		c.stats.Cached++
		prev.CheckedAt = c.Now()
		c.Cache.Pages[rawURL] = prev
		c.logf("  unchanged %s (%d cached event(s))", rawURL, len(prev.Events))
		return page, prev.Events
	}

	p := extract.NewPage(rawURL, page)
	var events []extract.Event
	if extract.Relevant(p.Title + "\n" + p.Text) {
		c.stats.Extracted++
		if events, err = c.Pipeline.Run(ctx, p); err != nil {
			c.fail(rawURL, err)
			return page, nil // not cached, so the next run retries it
		}
		c.logf("  %d event(s) from %s", len(events), rawURL)
	} else {
		c.stats.Skipped++
		c.logf("  no whisky keywords in %s", rawURL)
	}
	c.Cache.Pages[rawURL] = CacheEntry{Hash: hash, CheckedAt: c.Now(), Events: events}
	return page, events
}

// robotsFor fetches and memoises robots.txt per host. A missing robots.txt
// (any fetch error, e.g. 404) allows everything, as most crawlers do.
func (c *Crawler) robotsFor(ctx context.Context, u *url.URL) *robots {
	if r, ok := c.robots[u.Host]; ok {
		return r
	}
	robotsURL := u.Scheme + "://" + u.Host + "/robots.txt"
	body, err := c.get(ctx, u.Host, robotsURL)
	r := &robots{}
	if err == nil {
		r = parseRobots(body, c.UserAgent)
	}
	c.robots[u.Host] = r
	return r
}

// get fetches rawURL after waiting out the per-host delay.
func (c *Crawler) get(ctx context.Context, host, rawURL string) (string, error) {
	if last, ok := c.lastHit[host]; ok {
		if wait := c.Delay - c.Now().Sub(last); wait > 0 {
			c.Sleep(wait)
		}
	}
	c.lastHit[host] = c.Now()
	return c.Fetch.Get(ctx, rawURL)
}

func (c *Crawler) fail(rawURL string, err error) {
	c.stats.Errors++
	c.logf("  error %s: %v", rawURL, err)
}

func (c *Crawler) logf(format string, args ...interface{}) {
	fmt.Fprintf(c.Log, format+"\n", args...)
}

var hrefAttr = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*["']([^"']+)["']`)

// findLinks returns up to max distinct same-host links in page whose
// absolute URL matches pattern, in document order. Fragments are dropped.
func findLinks(page, base string, pattern *regexp.Regexp, max int) []string {
	b, err := url.Parse(base)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, m := range hrefAttr.FindAllStringSubmatch(page, -1) {
		ref, err := url.Parse(strings.TrimSpace(m[1]))
		if err != nil {
			continue
		}
		abs := b.ResolveReference(ref)
		abs.Fragment = ""
		s := abs.String()
		if abs.Host != b.Host || s == base || seen[s] || !pattern.MatchString(s) {
			continue
		}
		seen[s] = true
		out = append(out, s)
		if len(out) >= max {
			break
		}
	}
	return out
}
