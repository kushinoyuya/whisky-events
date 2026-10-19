package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/kushinoyuya/whisky-events/internal/llm"
)

// MaxTextRunes bounds the page text sent to the model. With num_ctx=4096 and
// Japanese text at roughly one token per character, this leaves room for the
// prompt and the JSON answer.
const MaxTextRunes = 2500

// Prompt builds the instruction for the model. Today's date is included so the
// model can resolve dates written without a year (e.g. "11月7日(土)").
func Prompt(text string, today time.Time) string {
	return `あなたはウイスキー関連イベントの情報を抽出するアシスタントです。
以下のWebページ本文から、ウイスキーに関するイベント(試飲会・フェスティバル・セミナー・蒸留所見学会など)を抽出してください。

ルール:
- 本文に書かれている情報だけを使う。推測で補わない。不明な項目は空文字にする。
- ウイスキーと無関係なイベント、すでに終了したと明記されたイベントは含めない。
- 日付は YYYY-MM-DD 形式。年が書かれていなければ、今日(` + today.Format(dateLayout) + `)以降で最も近い日付と解釈する。
- 1日だけのイベントは end_date を空にする。
- prefecture は都道府県名(例: 東京都)。
- イベントがなければ {"events": []} を返す。

出力は次のJSONのみ:
{"events": [{"title": "", "start_date": "", "end_date": "", "venue": "", "prefecture": "", "price": "", "summary": ""}]}

本文:
"""
` + text + `
"""`
}

// LLM asks a language model for the events in a page's text.
func LLM(ctx context.Context, g llm.Generator, p Page, today time.Time) ([]Event, error) {
	text := truncateRunes(strings.TrimSpace(p.Title+"\n"+p.Text), MaxTextRunes)
	raw, err := g.Generate(ctx, Prompt(text, today))
	if err != nil {
		return nil, err
	}
	events, err := ParseLLM(raw)
	if err != nil {
		return nil, fmt.Errorf("parse model output: %w", err)
	}
	for i := range events {
		events[i].Method = "llm"
	}
	return events, nil
}

// ParseLLM decodes the model's answer. Small models sometimes wrap JSON in
// code fences or prose, so we decode from the first '{' to the last '}'.
// Validation happens later in clean, same as for the other extractors.
func ParseLLM(raw string) ([]Event, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON object in %.100q", raw)
	}
	var out struct {
		Events []Event `json:"events"`
	}
	if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err != nil {
		return nil, err
	}
	return out.Events, nil
}
