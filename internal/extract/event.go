// Package extract finds whisky events in web pages.
//
// Extraction runs in stages, cheapest and most reliable first:
//  1. JSON-LD (schema.org Event) embedded in the page
//  2. rules over the page text (labels such as 日時/会場 + Japanese dates)
//  3. optionally an LLM, only when the first two find nothing
package extract

import (
	"strings"
	"time"

	"github.com/kushinoyuya/whisky-events/internal/htmltext"
)

// Event is one whisky event found on a page.
type Event struct {
	Title      string `json:"title"`
	StartDate  string `json:"start_date"`         // YYYY-MM-DD
	EndDate    string `json:"end_date,omitempty"` // YYYY-MM-DD, empty for one-day events
	Venue      string `json:"venue,omitempty"`
	Prefecture string `json:"prefecture,omitempty"`
	Price      string `json:"price,omitempty"`
	Summary    string `json:"summary,omitempty"`
	SourceURL  string `json:"source_url"`
	Method     string `json:"method"` // "jsonld", "rules" or "llm": how it was found
}

// Page is a fetched page in the forms the extractors need.
type Page struct {
	URL   string
	HTML  string
	Title string
	Text  string
}

// NewPage derives the title and plain text from raw HTML.
func NewPage(url, html string) Page {
	return Page{
		URL:   url,
		HTML:  html,
		Title: htmltext.Title(html),
		Text:  htmltext.Extract(html),
	}
}

const dateLayout = "2006-01-02"

var keywords = []string{
	"ウイスキー", "ウィスキー", "whisky", "whiskey",
	"蒸留所", "蒸溜所", "シングルモルト", "スコッチ", "バーボン",
}

// Relevant reports whether s mentions whisky.
func Relevant(s string) bool {
	s = strings.ToLower(s)
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// Upcoming keeps events that have not finished before today.
func Upcoming(events []Event, today time.Time) []Event {
	cutoff := today.Format(dateLayout)
	var out []Event
	for _, e := range events {
		last := e.EndDate
		if last == "" {
			last = e.StartDate
		}
		if last >= cutoff { // YYYY-MM-DD compares correctly as a string
			out = append(out, e)
		}
	}
	return out
}

// clean trims fields, drops events without a title or valid start date, and
// repairs end dates that are invalid or not after the start.
func clean(events []Event) []Event {
	var out []Event
	for _, e := range events {
		e.Title = strings.TrimSpace(e.Title)
		e.StartDate = strings.TrimSpace(e.StartDate)
		e.EndDate = strings.TrimSpace(e.EndDate)
		e.Venue = strings.TrimSpace(e.Venue)
		e.Prefecture = strings.TrimSpace(e.Prefecture)
		e.Price = strings.TrimSpace(e.Price)
		e.Summary = strings.TrimSpace(e.Summary)
		if e.Title == "" || !isDate(e.StartDate) {
			continue // a title and a start date are the minimum to be useful
		}
		if e.EndDate != "" && (!isDate(e.EndDate) || e.EndDate <= e.StartDate) {
			e.EndDate = ""
		}
		out = append(out, e)
	}
	return Dedupe(out)
}

// Dedupe drops events with the same title and start date, keeping the first.
// Stage 3 will merge near-duplicates (same event, differently worded titles).
func Dedupe(events []Event) []Event {
	var out []Event
	seen := map[string]bool{}
	for _, e := range events {
		key := e.Title + "|" + e.StartDate
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, e)
	}
	return out
}

func isDate(s string) bool {
	_, err := time.Parse(dateLayout, s)
	return err == nil
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
