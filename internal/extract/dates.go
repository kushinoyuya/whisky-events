package extract

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

const monthNames = `(january|february|march|april|may|june|july|august|september|october|november|december|jan|feb|mar|apr|jun|jul|aug|sept|sep|oct|nov|dec)`

var (
	// 2026年11月7日 / 11月7日 (year optional)
	jpDate = regexp.MustCompile(`(?:(\d{4})\s*年\s*)?(\d{1,2})\s*月\s*(\d{1,2})\s*日`)
	// 2026/11/07, 2026.11.7, 2026-11-07
	numDate = regexp.MustCompile(`(\d{4})[/.\-](\d{1,2})[/.\-](\d{1,2})`)
	// right after a jpDate: "(金)〜27日" means the 27th of the same month
	dayOnlyEnd = regexp.MustCompile(`^\s*(?:[(（][^)）]{1,4}[)）])?\s*[〜~\-–ー]\s*(\d{1,2})\s*日`)
	// November 29, 2026 / Nov 29 / June 14-15, 2026 (full names first: RE2 alternation is leftmost-first)
	enMDY = regexp.MustCompile(`(?i)\b` + monthNames + `\.?\s+(\d{1,2})(?:st|nd|rd|th)?(?:\s*[-–]\s*(\d{1,2})(?:st|nd|rd|th)?)?\b(?:,?\s+(\d{4}))?`)
	// 29 November 2026 / 14-15 June 2026
	enDMY = regexp.MustCompile(`(?i)\b(\d{1,2})(?:st|nd|rd|th)?(?:\s*[-–]\s*(\d{1,2})(?:st|nd|rd|th)?)?\s+` + monthNames + `\b\.?(?:,?\s+(\d{4}))?`)
)

var monthNum = map[string]int{
	"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12,
}

// normalizeDigits maps full-width digits and tildes to ASCII so one set of
// regexps covers both "１１月７日" and "11月7日".
func normalizeDigits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '０' && r <= '９':
			return r - '０' + '0'
		case r == '～' || r == '〰':
			return '〜'
		case r == '／':
			return '/'
		}
		return r
	}, s)
}

type dateHit struct {
	pos int
	t   time.Time
}

// FindDates returns every date mentioned in s, in order of appearance.
// Dates without a year are placed in today's year, or the next year if that
// would put them more than a month in the past (a December page announcing
// "1月15日" means next January).
func FindDates(s string, today time.Time) []time.Time {
	s = normalizeDigits(s)
	var hits []dateHit

	for _, m := range jpDate.FindAllStringSubmatchIndex(s, -1) {
		start, ok := resolve(group(s, m, 1), atoi(group(s, m, 2)), atoi(group(s, m, 3)), today)
		if !ok {
			continue
		}
		hits = append(hits, dateHit{m[0], start})
		if e := dayOnlyEnd.FindStringSubmatch(s[m[1]:]); e != nil {
			if end, ok := rangeEnd(start, atoi(e[1])); ok {
				hits = append(hits, dateHit{m[1], end})
			}
		}
	}
	for _, m := range numDate.FindAllStringSubmatchIndex(s, -1) {
		if t, ok := makeDate(atoi(group(s, m, 1)), atoi(group(s, m, 2)), atoi(group(s, m, 3))); ok {
			hits = append(hits, dateHit{m[0], t})
		}
	}
	for _, m := range enMDY.FindAllStringSubmatchIndex(s, -1) {
		hits = appendEnglish(hits, m[0], group(s, m, 4), group(s, m, 1), group(s, m, 2), group(s, m, 3), today)
	}
	for _, m := range enDMY.FindAllStringSubmatchIndex(s, -1) {
		hits = appendEnglish(hits, m[0], group(s, m, 4), group(s, m, 3), group(s, m, 1), group(s, m, 2), today)
	}

	// order by position so callers see dates as written
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0 && hits[j].pos < hits[j-1].pos; j-- {
			hits[j], hits[j-1] = hits[j-1], hits[j]
		}
	}
	out := make([]time.Time, len(hits))
	for i, h := range hits {
		out[i] = h.t
	}
	return out
}

// appendEnglish adds an English date and, for "14-15", its end day.
func appendEnglish(hits []dateHit, pos int, year, month, day, endDay string, today time.Time) []dateHit {
	start, ok := resolve(year, monthNum[strings.ToLower(month)[:3]], atoi(day), today)
	if !ok {
		return hits
	}
	hits = append(hits, dateHit{pos, start})
	if endDay != "" {
		if end, ok := rangeEnd(start, atoi(endDay)); ok {
			hits = append(hits, dateHit{pos + 1, end})
		}
	}
	return hits
}

// resolve builds a date whose year may be missing (year == "").
func resolve(year string, month, day int, today time.Time) (time.Time, bool) {
	y := today.Year()
	if year != "" {
		y = atoi(year)
	}
	t, ok := makeDate(y, month, day)
	if ok && year == "" && t.Before(today.AddDate(0, -1, 0)) {
		t = t.AddDate(1, 0, 0)
	}
	return t, ok
}

// rangeEnd interprets a bare end day relative to start: "30日〜2日" crosses
// into the next month.
func rangeEnd(start time.Time, day int) (time.Time, bool) {
	m := int(start.Month())
	if day < start.Day() {
		m++
	}
	return makeDate(start.Year(), m, day)
}

// makeDate builds a date, rejecting impossible ones like 2月30日 (which
// time.Date would silently roll over into March).
func makeDate(y, m, d int) (time.Time, bool) {
	if m > 12 { // allow 13 for the month-crossing case in rangeEnd
		y, m = y+1, m-12
	}
	if y < 2000 || y > 2100 || m < 1 || d < 1 {
		return time.Time{}, false
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

// group returns submatch i of an index match, or "" if it didn't take part.
func group(s string, m []int, i int) string {
	if m[2*i] < 0 {
		return ""
	}
	return s[m[2*i]:m[2*i+1]]
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
