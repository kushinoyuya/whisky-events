package extract

import (
	"regexp"
	"strings"
	"time"
)

// label matches a heading line such as "日時", "【会場】", "■料金：3,000円".
// A separator (or end of line) is required after the label so that body text
// like "会場内で販売" isn't mistaken for a "会場" label.
var label = regexp.MustCompile(`^[【\[■●◆◇・]?\s*(開催日時|開催日|日時|日程|期間|会期|開催場所|会場|場所|入場料|参加費|料金|価格|チケット料金)\s*(?:(?:[】\]]\s*[:：]?|[:：]|\s)\s*(.*))?$`)

// enLabel matches English headings. Unlike the Japanese form it needs a colon
// or a bare line, since "Location of the venue..." is ordinary prose.
var enLabel = regexp.MustCompile(`(?i)^(dates?|when|venue|location|where|price|admission|entry fee|tickets?)\s*(?::\s*(.*))?$`)

var labelKind = map[string]string{
	"開催日時": "date", "開催日": "date", "日時": "date", "日程": "date", "期間": "date", "会期": "date",
	"開催場所": "venue", "会場": "venue", "場所": "venue",
	"入場料": "price", "参加費": "price", "料金": "price", "価格": "price", "チケット料金": "price",
	"date": "date", "dates": "date", "when": "date",
	"venue": "venue", "location": "venue", "where": "venue",
	"price": "price", "admission": "price", "entry fee": "price", "ticket": "price", "tickets": "price",
}

// matchLabel returns the kind and inline value of a label line.
func matchLabel(line string) (kind, value string, ok bool) {
	m := label.FindStringSubmatch(line)
	if m == nil {
		m = enLabel.FindStringSubmatch(line)
	}
	if m == nil {
		return "", "", false
	}
	return labelKind[strings.ToLower(m[1])], strings.TrimSpace(m[2]), true
}

// maxValueLines bounds how many lines after a bare label belong to it.
const maxValueLines = 3

// Rules extracts at most one event per page from labelled text. It is meant
// for the common "one event, one page" layout; listing pages are better
// served by JSON-LD or the LLM.
func Rules(p Page, today time.Time) []Event {
	if !Relevant(p.Title + "\n" + p.Text) {
		return nil
	}
	sections := labelledSections(strings.Split(p.Text, "\n"))

	var dates []time.Time
	for _, v := range sections["date"] {
		dates = append(dates, FindDates(v, today)...)
	}
	if len(dates) == 0 {
		// no 日時 label: take the first upcoming date anywhere on the page
		for _, d := range FindDates(p.Text, today) {
			if !d.Before(today) {
				dates = []time.Time{d}
				break
			}
		}
	}
	if len(dates) == 0 {
		return nil
	}
	start, end := dates[0], dates[0]
	for _, d := range dates[1:] {
		if d.Before(start) {
			start = d
		}
		if d.After(end) {
			end = d
		}
	}

	e := Event{
		Title:     pageTitle(p),
		StartDate: start.Format(dateLayout),
		Venue:     firstOf(sections["venue"]),
		Price:     firstOf(sections["price"]),
		Method:    "rules",
	}
	if end.After(start) {
		e.EndDate = end.Format(dateLayout)
	}
	e.Prefecture = prefectureIn(e.Venue)
	if e.Prefecture == "" {
		e.Prefecture = prefectureIn(e.Title)
	}
	return []Event{e}
}

// labelledSections groups value lines under their label kind. A value is
// either on the label line ("会場：○○") or on the lines after a bare label.
func labelledSections(lines []string) map[string][]string {
	out := map[string][]string{}
	kind, taken := "", 0
	for _, line := range lines {
		if k, v, ok := matchLabel(line); ok {
			kind, taken = k, 0
			if v != "" {
				out[kind] = append(out[kind], v)
				taken++
			}
			continue
		}
		if kind != "" && taken < maxValueLines && line != "" {
			out[kind] = append(out[kind], line)
			taken++
		}
	}
	return out
}

// pageTitle prefers the first text line (usually the <h1>) when it looks
// like an event name, falling back to <title> minus any " | Site" suffix.
func pageTitle(p Page) string {
	if i := strings.IndexByte(p.Text, '\n'); i > 0 {
		h := p.Text[:i]
		if Relevant(h) && len([]rune(h)) <= 60 {
			return h
		}
	}
	t := p.Title
	for _, sep := range []string{" | ", "｜", " - ", " – "} {
		if i := strings.Index(t, sep); i > 0 {
			t = t[:i]
		}
	}
	return t
}

func firstOf(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return vs[0]
}

var prefectures = []string{
	"北海道", "青森県", "岩手県", "宮城県", "秋田県", "山形県", "福島県",
	"茨城県", "栃木県", "群馬県", "埼玉県", "千葉県", "東京都", "神奈川県",
	"新潟県", "富山県", "石川県", "福井県", "山梨県", "長野県", "岐阜県",
	"静岡県", "愛知県", "三重県", "滋賀県", "京都府", "大阪府", "兵庫県",
	"奈良県", "和歌山県", "鳥取県", "島根県", "岡山県", "広島県", "山口県",
	"徳島県", "香川県", "愛媛県", "高知県", "福岡県", "佐賀県", "長崎県",
	"熊本県", "大分県", "宮崎県", "鹿児島県", "沖縄県",
}

// romaji lists prefectures in the same order as prefectures, for English pages.
var romaji = []string{
	"hokkaido", "aomori", "iwate", "miyagi", "akita", "yamagata", "fukushima",
	"ibaraki", "tochigi", "gunma", "saitama", "chiba", "tokyo", "kanagawa",
	"niigata", "toyama", "ishikawa", "fukui", "yamanashi", "nagano", "gifu",
	"shizuoka", "aichi", "mie", "shiga", "kyoto", "osaka", "hyogo",
	"nara", "wakayama", "tottori", "shimane", "okayama", "hiroshima", "yamaguchi",
	"tokushima", "kagawa", "ehime", "kochi", "fukuoka", "saga", "nagasaki",
	"kumamoto", "oita", "miyazaki", "kagoshima", "okinawa",
}

// cities maps venues' cities that don't contain their prefecture's name.
// A slice, not a map, so lookups are deterministic when several match.
var cities = []struct{ name, pref string }{
	{"札幌", "北海道"}, {"仙台", "宮城県"}, {"横浜", "神奈川県"}, {"川崎", "神奈川県"},
	{"秩父", "埼玉県"}, {"名古屋", "愛知県"}, {"神戸", "兵庫県"}, {"博多", "福岡県"},
	{"那覇", "沖縄県"}, {"渋谷", "東京都"}, {"恵比寿", "東京都"}, {"sapporo", "北海道"},
	{"sendai", "宮城県"}, {"yokohama", "神奈川県"}, {"chichibu", "埼玉県"}, {"nagoya", "愛知県"},
	{"kobe", "兵庫県"}, {"hakata", "福岡県"}, {"naha", "沖縄県"}, {"shibuya", "東京都"},
	{"ebisu", "東京都"},
}

var romajiRe = func() *regexp.Regexp {
	names := append([]string{}, romaji...)
	for _, c := range cities {
		if c.name[0] < 0x80 {
			names = append(names, c.name)
		}
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(names, "|") + `)\b`)
}()

// prefectureIn finds a prefecture in s: full name ("東京都"), then short
// name ("東京"), then a known city ("横浜"), then romaji ("Okinawa").
func prefectureIn(s string) string {
	for _, p := range prefectures {
		if strings.Contains(s, p) {
			return p
		}
	}
	for _, p := range prefectures {
		if p == "北海道" {
			continue
		}
		r := []rune(p)
		short := string(r[:len(r)-1]) // 京都府 -> 京都, 神奈川県 -> 神奈川
		if strings.Contains(s, short) {
			return p
		}
	}
	for _, c := range cities {
		if c.name[0] >= 0x80 && strings.Contains(s, c.name) {
			return c.pref
		}
	}
	if m := romajiRe.FindString(s); m != "" {
		m = strings.ToLower(m)
		for _, c := range cities {
			if c.name == m {
				return c.pref
			}
		}
		for i, r := range romaji {
			if r == m {
				return prefectures[i]
			}
		}
	}
	return ""
}
