// Package crawl visits the configured sites politely (robots.txt, per-host
// delay) and extracts events, caching results by page content.
package crawl

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"regexp"
)

// DefaultMaxLinks caps how many detail pages are followed from one listing.
const DefaultMaxLinks = 20

// Site is one crawl target from sites.json.
type Site struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Kind    string `json:"kind,omitempty"`
	Enabled bool   `json:"enabled"`
	// Follow is a regexp over absolute URLs. When set, the page at URL is
	// treated as a listing and matching same-host links are crawled too.
	Follow   string `json:"follow,omitempty"`
	MaxLinks int    `json:"max_links,omitempty"`
	Note     string `json:"note,omitempty"`

	follow *regexp.Regexp
}

// Config is the contents of sites.json.
type Config struct {
	Sites []Site `json:"sites"`
}

// LoadConfig reads and validates a sites.json file.
func LoadConfig(path string) (*Config, error) {
	b, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i := range c.Sites {
		s := &c.Sites[i]
		if s.URL == "" {
			return nil, fmt.Errorf("%s: site %q has no url", path, s.Name)
		}
		if s.Follow != "" {
			if s.follow, err = regexp.Compile(s.Follow); err != nil {
				return nil, fmt.Errorf("%s: site %q: bad follow pattern: %w", path, s.Name, err)
			}
		}
		if s.MaxLinks <= 0 {
			s.MaxLinks = DefaultMaxLinks
		}
	}
	return &c, nil
}
