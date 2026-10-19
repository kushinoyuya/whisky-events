package crawl

import (
	"context"
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kushinoyuya/whisky-events/internal/extract"
)

var today = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

// mapFetcher serves pages from memory and records the request order.
type mapFetcher struct {
	pages    map[string]string
	requests []string
}

func (f *mapFetcher) Get(_ context.Context, url string) (string, error) {
	f.requests = append(f.requests, url)
	if p, ok := f.pages[url]; ok {
		return p, nil
	}
	return "", errors.New("404")
}

type countingLLM struct{ calls int }

func (l *countingLLM) Generate(context.Context, string) (string, error) {
	l.calls++
	return `{"events":[{"title":"蒸留所見学ツアー","start_date":"2026-11-20"}]}`, nil
}

const (
	listing = `<html><body><h1>ウイスキーイベント一覧</h1>
<a href="/events/1">A</a> <a href="/events/2#top">B</a> <a href="/events/1">A again</a>
<a href="/about">about</a> <a href="https://other.example/events/9">offsite</a>
<a href="/events/private">private</a></body></html>`
	event1    = `<h1>ウイスキー試飲会</h1><p>日時：2026年11月14日</p><p>会場：福岡市内</p>`
	event2    = `<h1>Whisky Night</h1><p>Date: November 29, 2026</p><p>Venue: Okinawa Harbor View Hotel</p>`
	robotsTxt = "User-agent: *\nDisallow: /events/private\n"
)

func newCrawler(f Fetcher, llm *countingLLM) *Crawler {
	pl := &extract.Pipeline{Now: func() time.Time { return today }}
	if llm != nil {
		pl.LLM = llm
	}
	return &Crawler{
		Fetch:     f,
		Pipeline:  pl,
		Cache:     &Cache{Pages: map[string]CacheEntry{}},
		UserAgent: "whisky-events/0.1",
		Delay:     2 * time.Second,
		Now:       func() time.Time { return today },
		Sleep:     func(time.Duration) {},
	}
}

func site(name, url, follow string) Site {
	s := Site{Name: name, URL: url, Enabled: true, MaxLinks: DefaultMaxLinks}
	if follow != "" {
		s.follow = regexp.MustCompile(follow)
	}
	return s
}

func TestCrawlFollowsListingAndRespectsRobots(t *testing.T) {
	f := &mapFetcher{pages: map[string]string{
		"https://ex.jp/robots.txt": robotsTxt,
		"https://ex.jp/list":       listing,
		"https://ex.jp/events/1":   event1,
		"https://ex.jp/events/2":   event2,
	}}
	c := newCrawler(f, nil)
	var slept []time.Duration
	c.Sleep = func(d time.Duration) { slept = append(slept, d) }

	events, stats := c.Run(context.Background(), []Site{site("ex", "https://ex.jp/list", `/events/`)})

	if len(events) != 2 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].StartDate != "2026-11-14" || events[0].Prefecture != "福岡県" {
		t.Errorf("event1 = %+v", events[0])
	}
	if events[1].Title != "Whisky Night" || events[1].StartDate != "2026-11-29" || events[1].Prefecture != "沖縄県" {
		t.Errorf("event2 = %+v", events[1])
	}
	if stats.Blocked != 1 {
		t.Errorf("blocked = %d, want 1 (/events/private)", stats.Blocked)
	}
	for _, r := range f.requests {
		if strings.Contains(r, "other.example") || strings.Contains(r, "/about") || strings.Contains(r, "private") {
			t.Errorf("should not request %s", r)
		}
	}
	// robots, list, events/1, events/2 on one host: a delay before each after the first
	if len(slept) != 3 {
		t.Errorf("slept %d times, want 3", len(slept))
	}
}

func TestCrawlCacheSkipsUnchangedPages(t *testing.T) {
	// whisky-related but nothing the rules can read, so the LLM is used
	f := &mapFetcher{pages: map[string]string{
		"https://ex.jp/tour": `<p>ウイスキー蒸留所の見学ツアーを来月開催予定です。</p>`,
	}}
	llm := &countingLLM{}
	c := newCrawler(f, llm)
	sites := []Site{site("tour", "https://ex.jp/tour", "")}

	first, _ := c.Run(context.Background(), sites)
	second, stats := c.Run(context.Background(), sites)

	if llm.calls != 1 {
		t.Errorf("llm calls = %d, want 1 (second run should hit the cache)", llm.calls)
	}
	if len(first) != 1 || len(second) != 1 || stats.Cached != 1 {
		t.Errorf("first %v, second %v, stats %+v", first, second, stats)
	}

	f.pages["https://ex.jp/tour"] += "<p>更新</p>"
	c.Run(context.Background(), sites)
	if llm.calls != 2 {
		t.Errorf("llm calls = %d, want 2 after the page changed", llm.calls)
	}
}

func TestCrawlCachedEventsExpire(t *testing.T) {
	f := &mapFetcher{pages: map[string]string{"https://ex.jp/e": event1}}
	c := newCrawler(f, nil)
	sites := []Site{site("e", "https://ex.jp/e", "")}
	c.Run(context.Background(), sites)

	c.Now = func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) }
	events, stats := c.Run(context.Background(), sites)
	if len(events) != 0 || stats.Cached != 1 {
		t.Errorf("events %v, stats %+v: cached event from 11/14 should be dropped by 12/1", events, stats)
	}
}

func TestCrawlSkipsDisabledAndUnrelated(t *testing.T) {
	f := &mapFetcher{pages: map[string]string{
		"https://ex.jp/wine": `<p>ワイン試飲会 日時：2026年11月3日</p>`,
	}}
	c := newCrawler(f, nil)
	off := site("off", "https://ex.jp/off", "")
	off.Enabled = false

	events, stats := c.Run(context.Background(), []Site{off, site("wine", "https://ex.jp/wine", "")})
	if len(events) != 0 || stats.Skipped != 1 || stats.Fetched != 1 {
		t.Errorf("events %v, stats %+v", events, stats)
	}
	for _, r := range f.requests {
		if strings.HasSuffix(r, "/off") {
			t.Error("disabled site was fetched")
		}
	}
}

func TestCrawlCountsErrorsAndContinues(t *testing.T) {
	f := &mapFetcher{pages: map[string]string{"https://ok.jp/e": event1}}
	c := newCrawler(f, nil)
	events, stats := c.Run(context.Background(), []Site{
		site("down", "https://down.jp/e", ""),
		site("ok", "https://ok.jp/e", ""),
	})
	if stats.Errors != 1 || len(events) != 1 {
		t.Errorf("events %v, stats %+v", events, stats)
	}
}

func TestParseRobots(t *testing.T) {
	body := `# comment
User-agent: Googlebot
Disallow: /

User-agent: whisky-events
User-agent: otherbot
Disallow: /private
Allow: /private/events

User-agent: *
Disallow: /admin
Disallow:
`
	r := parseRobots(body, "whisky-events/0.1 (+https://github.com/kushinoyuya/whisky-events)")
	cases := map[string]bool{
		"/":                 true,
		"/admin":            true, // the * group doesn't apply when ours exists
		"/private":          false,
		"/private/x":        false,
		"/private/events/1": true, // longer Allow wins
	}
	for path, want := range cases {
		if got := r.allowed(path); got != want {
			t.Errorf("allowed(%q) = %v, want %v", path, got, want)
		}
	}

	star := parseRobots(body, "somebot/1.0")
	if star.allowed("/admin") || !star.allowed("/private") {
		t.Error("unknown agents should get the * group")
	}
	if !parseRobots("", "whisky-events").allowed("/anything") {
		t.Error("empty robots.txt should allow everything")
	}
}

func TestCacheRoundTrip(t *testing.T) {
	dir, err := ioutil.TempDir("", "whisky-cache")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "sub", "cache.json")

	c, err := LoadCache(path)
	if err != nil || len(c.Pages) != 0 {
		t.Fatalf("missing cache: %v, %v", c, err)
	}
	c.Pages["u"] = CacheEntry{Hash: "h", Events: []extract.Event{{Title: "t", StartDate: "2026-11-01"}}}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	c2, err := LoadCache(path)
	if err != nil || c2.Pages["u"].Hash != "h" || c2.Pages["u"].Events[0].Title != "t" {
		t.Errorf("reloaded = %+v, %v", c2, err)
	}
}

func TestLoadConfig(t *testing.T) {
	c, err := LoadConfig("../../sites.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Sites) == 0 {
		t.Fatal("no sites")
	}
	for _, s := range c.Sites {
		if s.MaxLinks <= 0 {
			t.Errorf("%s: MaxLinks default not applied", s.Name)
		}
		if s.Follow != "" && s.follow == nil {
			t.Errorf("%s: follow pattern not compiled", s.Name)
		}
	}
}
