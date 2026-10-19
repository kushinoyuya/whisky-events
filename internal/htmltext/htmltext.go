// Package htmltext reduces an HTML page to readable plain text for the LLM.
//
// It is deliberately simple (regexp-based, stdlib only). Feeding the model
// less markup means fewer tokens, which matters on CPU-only inference.
package htmltext

import (
	"html"
	"regexp"
	"strings"
)

var (
	comments   = regexp.MustCompile(`(?s)<!--.*?-->`)
	blockTags  = regexp.MustCompile(`(?i)</?(p|div|br|li|tr|h[1-6]|section|article|dd|dt|table)\b[^>]*>`)
	anyTag     = regexp.MustCompile(`(?s)<[^>]+>`)
	spaces     = regexp.MustCompile(`[ \t\x{3000}\x{00a0}]+`)
	blankLines = regexp.MustCompile(`\n\s*\n+`)
	titleTag   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

// dropBlocks removes non-content elements with their bodies. One regexp per
// tag because Go's RE2 has no backreferences to match the closing tag.
// Inner elements go first so e.g. </svg> inside <nav> can't end the match early.
var dropBlocks = func() []*regexp.Regexp {
	var rs []*regexp.Regexp
	for _, tag := range []string{"script", "style", "noscript", "svg", "iframe", "head", "nav", "footer"} {
		rs = append(rs, regexp.MustCompile(`(?is)<`+tag+`\b.*?</`+tag+`\s*>`))
	}
	return rs
}()

// Title returns the contents of <title>, or "" if absent.
func Title(page string) string {
	m := titleTag.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(html.UnescapeString(anyTag.ReplaceAllString(m[1], "")))
}

// Extract converts HTML to text, keeping line breaks at block boundaries.
func Extract(page string) string {
	s := comments.ReplaceAllString(page, "")
	for _, re := range dropBlocks {
		s = re.ReplaceAllString(s, "")
	}
	s = blockTags.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = spaces.ReplaceAllString(s, " ")

	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	s = strings.Join(lines, "\n")
	s = blankLines.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}
