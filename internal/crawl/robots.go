package crawl

import (
	"strings"
)

// robots holds the Disallow/Allow rules that apply to our user agent.
// It implements the common subset of robots.txt: user-agent groups and
// path-prefix Allow/Disallow, longest match wins. Wildcards are not supported.
type robots struct {
	allow    []string
	disallow []string
}

// parseRobots picks the group for agent (matched by product token, e.g.
// "whisky-events"), falling back to "*".
func parseRobots(body, agent string) *robots {
	// "whisky-events/0.1 (+https://...)" -> "whisky-events"
	token := strings.ToLower(strings.TrimSpace(strings.SplitN(agent, "/", 2)[0]))
	groups := map[string]*robots{}
	var current []*robots
	inRules := false

	for _, line := range strings.Split(body, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "user-agent":
			if inRules { // a user-agent after rules starts a new group
				current, inRules = nil, false
			}
			ua := strings.ToLower(val)
			if groups[ua] == nil {
				groups[ua] = &robots{}
			}
			current = append(current, groups[ua])
		case "allow", "disallow":
			inRules = true
			if val == "" {
				continue // "Disallow:" with no path allows everything
			}
			for _, g := range current {
				if key == "allow" {
					g.allow = append(g.allow, val)
				} else {
					g.disallow = append(g.disallow, val)
				}
			}
		}
	}

	if g := groups[token]; g != nil && token != "" {
		return g
	}
	if g := groups["*"]; g != nil {
		return g
	}
	return &robots{}
}

// allowed reports whether path (with query) may be fetched.
func (r *robots) allowed(path string) bool {
	best, ok := 0, true
	for _, p := range r.disallow {
		if strings.HasPrefix(path, p) && len(p) > best {
			best, ok = len(p), false
		}
	}
	for _, p := range r.allow {
		if strings.HasPrefix(path, p) && len(p) >= best {
			best, ok = len(p), true
		}
	}
	return ok
}
