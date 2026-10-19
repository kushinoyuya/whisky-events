package extract

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

var ldScript = regexp.MustCompile(`(?is)<script[^>]*type\s*=\s*["']?application/ld\+json["']?[^>]*>(.*?)</script\s*>`)

// JSONLD extracts schema.org Event objects (Event, Festival, FoodEvent, ...)
// embedded in the page. Malformed blocks are skipped, not fatal: one broken
// script tag shouldn't hide the events in another.
func JSONLD(p Page) []Event {
	var events []Event
	for _, m := range ldScript.FindAllStringSubmatch(p.HTML, -1) {
		var v interface{}
		if err := json.Unmarshal([]byte(strings.TrimSpace(m[1])), &v); err != nil {
			continue
		}
		walkLD(v, func(obj map[string]interface{}) {
			e := ldEvent(obj)
			if Relevant(e.Title+" "+e.Summary) || Relevant(p.Title) {
				events = append(events, e)
			}
		})
	}
	return events
}

// walkLD visits every Event-typed object, descending into arrays and @graph.
func walkLD(v interface{}, visit func(map[string]interface{})) {
	switch x := v.(type) {
	case []interface{}:
		for _, item := range x {
			walkLD(item, visit)
		}
	case map[string]interface{}:
		if isEventType(x["@type"]) {
			visit(x)
			return
		}
		if g, ok := x["@graph"]; ok {
			walkLD(g, visit)
		}
	}
}

func isEventType(t interface{}) bool {
	switch x := t.(type) {
	case string:
		x = strings.TrimPrefix(x, "schema:")
		return strings.HasSuffix(x, "Event") || x == "Festival"
	case []interface{}:
		for _, item := range x {
			if isEventType(item) {
				return true
			}
		}
	}
	return false
}

func ldEvent(obj map[string]interface{}) Event {
	e := Event{
		Title:     str(obj["name"]),
		StartDate: ldDate(str(obj["startDate"])),
		EndDate:   ldDate(str(obj["endDate"])),
		Summary:   truncateRunes(str(obj["description"]), 120),
		Method:    "jsonld",
	}
	loc := first(obj["location"])
	if m, ok := loc.(map[string]interface{}); ok {
		e.Venue = str(m["name"])
		switch a := first(m["address"]).(type) {
		case map[string]interface{}:
			e.Prefecture = prefectureIn(str(a["addressRegion"]) + str(a["addressLocality"]) + str(a["streetAddress"]))
		case string:
			e.Prefecture = prefectureIn(a)
		}
	} else {
		e.Venue = str(loc)
	}
	if e.Prefecture == "" {
		e.Prefecture = prefectureIn(e.Venue)
	}
	if m, ok := first(obj["offers"]).(map[string]interface{}); ok {
		e.Price = ldPrice(m)
	}
	return e
}

// ldDate keeps the date part of an ISO 8601 date-time.
func ldDate(s string) string {
	if len(s) >= 10 && isDate(s[:10]) {
		return s[:10]
	}
	return s
}

func ldPrice(offer map[string]interface{}) string {
	unit := func(cur string) string {
		if cur == "" || cur == "JPY" {
			return "円"
		}
		return " " + cur
	}
	cur := str(offer["priceCurrency"])
	if p := str(offer["price"]); p != "" {
		return p + unit(cur)
	}
	lo, hi := str(offer["lowPrice"]), str(offer["highPrice"])
	if lo != "" && hi != "" {
		return lo + unit(cur) + "〜" + hi + unit(cur)
	}
	return ""
}

// first unwraps a one-or-many JSON-LD value to its first element.
func first(v interface{}) interface{} {
	if a, ok := v.([]interface{}); ok {
		if len(a) == 0 {
			return nil
		}
		return a[0]
	}
	return v
}

// str renders strings and numbers (prices are often numbers) as text.
func str(v interface{}) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%f", x), "0"), ".")
	}
	return ""
}
