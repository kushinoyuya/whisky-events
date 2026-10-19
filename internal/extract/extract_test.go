package extract

import (
	"context"
	"io/ioutil"
	"reflect"
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return today }

func loadPage(t *testing.T, name string) Page {
	t.Helper()
	b, err := ioutil.ReadFile("../../testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return NewPage("https://example.jp/"+name, string(b))
}

func TestPipelineRulesOnSample(t *testing.T) {
	pl := &Pipeline{Now: fixedNow}
	got, err := pl.Run(context.Background(), loadPage(t, "sample_event.html"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Event{{
		Title:      "ウイスキーフェスティバル2026 in 東京",
		StartDate:  "2026-11-06",
		EndDate:    "2026-11-07",
		Venue:      "東京国際フォーラム ホールE(東京都千代田区)",
		Prefecture: "東京都",
		Price:      "3,500円〜5,000円(税込)",
		SourceURL:  "https://example.jp/sample_event.html",
		Method:     "rules",
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestPipelinePrefersJSONLD(t *testing.T) {
	pl := &Pipeline{Now: fixedNow}
	got, err := pl.Run(context.Background(), loadPage(t, "jsonld_event.html"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1 (past one dropped): %+v", len(got), got)
	}
	e := got[0]
	if e.Method != "jsonld" || e.StartDate != "2026-12-05" || e.EndDate != "" ||
		e.Venue != "サンプルバー 札幌店" || e.Prefecture != "北海道" || e.Price != "4000円" {
		t.Errorf("event = %+v", e)
	}
}

type fakeLLM struct {
	answer string
	calls  int
	prompt string
}

func (f *fakeLLM) Generate(_ context.Context, prompt string) (string, error) {
	f.calls++
	f.prompt = prompt
	return f.answer, nil
}

func TestPipelineFallsBackToLLM(t *testing.T) {
	f := &fakeLLM{answer: "```json\n{\"events\":[{\"title\":\"蒸留所見学ツアー\",\"start_date\":\"2026-11-20\"}]}\n```"}
	pl := &Pipeline{LLM: f, Now: fixedNow}
	// whisky-related, but no labels or dates the rules can use
	p := NewPage("u", "<p>ウイスキー蒸留所の見学ツアーを来月下旬に開催予定です。</p>")

	got, err := pl.Run(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Method != "llm" || got[0].SourceURL != "u" {
		t.Errorf("got %+v", got)
	}
	if !strings.Contains(f.prompt, "2026-10-19") {
		t.Error("prompt should include today's date for year-less dates")
	}
}

func TestPipelineSkipsLLMForUnrelatedPages(t *testing.T) {
	f := &fakeLLM{answer: `{"events":[]}`}
	pl := &Pipeline{LLM: f, Now: fixedNow}
	got, err := pl.Run(context.Background(), NewPage("u", "<p>ワインの試飲会 11月3日</p>"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || f.calls != 0 {
		t.Errorf("got %+v, llm calls %d", got, f.calls)
	}
}

func TestLLMTruncatesLongText(t *testing.T) {
	f := &fakeLLM{answer: `{"events":[]}`}
	p := Page{Text: strings.Repeat("鯨", MaxTextRunes+500)}
	if _, err := LLM(context.Background(), f, p, today); err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(f.prompt, "鯨"); n != MaxTextRunes {
		t.Errorf("prompt has %d runes of body", n)
	}
}

func TestParseLLMRejectsNonJSON(t *testing.T) {
	if _, err := ParseLLM("イベントはありません"); err == nil {
		t.Error("want error for non-JSON output")
	}
}

func TestClean(t *testing.T) {
	got := clean([]Event{
		{Title: "ok", StartDate: "2026-11-07", EndDate: "2026-11-08"},
		{Title: "ok", StartDate: "2026-11-07"}, // duplicate
		{Title: " ", StartDate: "2026-11-07"},
		{Title: "bad date", StartDate: "11月7日"},
		{Title: "same", StartDate: "2026-11-07", EndDate: "2026-11-07"},
		{Title: "before", StartDate: "2026-11-07", EndDate: "2026-11-01"},
		{Title: "junk", StartDate: "2026-11-07", EndDate: "未定"},
	})
	var summary []string
	for _, e := range got {
		summary = append(summary, e.Title+":"+e.EndDate)
	}
	want := "ok:2026-11-08,same:,before:,junk:"
	if strings.Join(summary, ",") != want {
		t.Errorf("clean = %v, want %s", summary, want)
	}
}

func TestUpcoming(t *testing.T) {
	got := Upcoming([]Event{
		{Title: "past", StartDate: "2026-10-01"},
		{Title: "today", StartDate: "2026-10-19"},
		{Title: "ongoing", StartDate: "2026-10-10", EndDate: "2026-10-25"},
	}, today)
	if len(got) != 2 || got[0].Title != "today" || got[1].Title != "ongoing" {
		t.Errorf("upcoming = %+v", got)
	}
}

func TestFindDates(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"2026年11月7日(土)", []string{"2026-11-07"}},
		{"１１月７日", []string{"2026-11-07"}},
		{"6月26日(金)〜27日(土)", []string{"2027-06-26", "2027-06-27"}}, // >1 month past → next year
		{"10月30日～2日", []string{"2026-10-30", "2026-11-02"}},
		{"12月30日〜2日", []string{"2026-12-30", "2027-01-02"}},
		{"開催：2026/12/05 18:00", []string{"2026-12-05"}},
		{"2月30日", nil},
		{"17:00〜19:30", nil},
		{"10月1日", []string{"2026-10-01"}}, // within a month: kept in this year
	}
	for _, c := range cases {
		var got []string
		for _, d := range FindDates(c.in, today) {
			got = append(got, d.Format(dateLayout))
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("FindDates(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLabelledSections(t *testing.T) {
	lines := []string{
		"【日時】2026年11月7日",
		"会場内で限定ボトルを販売", // body text, not a label
		"会場",
		"東京国際フォーラム",
		"■料金：3,000円",
	}
	got := labelledSections(lines)
	want := map[string][]string{
		"date":  {"2026年11月7日", "会場内で限定ボトルを販売"},
		"venue": {"東京国際フォーラム"},
		"price": {"3,000円"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labelledSections = %v, want %v", got, want)
	}
}

func TestPrefectureIn(t *testing.T) {
	cases := map[string]string{
		"大阪府大阪市":             "大阪府",
		"ウイスキーフェスティバル in 東京": "東京都",
		"京都の蒸留所":             "京都府",
		"東京都内":               "東京都", // full name beats short "京都"
		"海外":                 "",
	}
	for in, want := range cases {
		if got := prefectureIn(in); got != want {
			t.Errorf("prefectureIn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrefectureInEnglishAndCities(t *testing.T) {
	cases := map[string]string{
		"Okinawa Harbor View Hotel":   "沖縄県",
		"Marine Messe Fukuoka Hall B": "福岡県",
		"Pacifico Yokohama":           "神奈川県",
		"パシフィコ横浜":                     "神奈川県",
		"秩父神社":                        "埼玉県",
		"Nagaokakyo":                  "", // word boundary: not "Kyoto"
	}
	for in, want := range cases {
		if got := prefectureIn(in); got != want {
			t.Errorf("prefectureIn(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFindDatesEnglish(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"Date: November 29, 2026", []string{"2026-11-29"}},
		{"Sun, Nov 29th 2026", []string{"2026-11-29"}},
		{"June 14-15, 2027", []string{"2027-06-14", "2027-06-15"}},
		{"29 November 2026", []string{"2026-11-29"}},
		{"Dec 5", []string{"2026-12-05"}},
		{"June 2026", nil},         // no day
		{"decided 3 options", nil}, // "dec" must be a whole word
	}
	for _, c := range cases {
		var got []string
		for _, d := range FindDates(c.in, today) {
			got = append(got, d.Format(dateLayout))
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("FindDates(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestEnglishLabels(t *testing.T) {
	got := labelledSections([]string{
		"Date: November 29, 2026",
		"Location of the festival is near the port.", // prose, not a label
		"Venue",
		"Okinawa Harbor View Hotel",
		"Admission: 5,000 yen",
	})
	want := map[string][]string{
		"date":  {"November 29, 2026", "Location of the festival is near the port."},
		"venue": {"Okinawa Harbor View Hotel"},
		"price": {"5,000 yen"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("labelledSections = %v, want %v", got, want)
	}
}
