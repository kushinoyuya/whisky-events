package extract

import (
	"context"
	"time"

	"github.com/kushinoyuya/whisky-events/internal/llm"
)

// Pipeline runs the extractors in order and returns the first non-empty,
// validated, still-upcoming result.
type Pipeline struct {
	LLM llm.Generator    // optional; nil disables the LLM stage
	Now func() time.Time // injectable for tests
}

// Run extracts events from p. Every event gets p.URL as its source so
// results can always be traced back to the page.
func (pl *Pipeline) Run(ctx context.Context, p Page) ([]Event, error) {
	now := time.Now
	if pl.Now != nil {
		now = pl.Now
	}
	t := now()
	today := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)

	stages := []func() ([]Event, error){
		func() ([]Event, error) { return JSONLD(p), nil },
		func() ([]Event, error) { return Rules(p, today), nil },
	}
	if pl.LLM != nil {
		stages = append(stages, func() ([]Event, error) {
			if !Relevant(p.Title + "\n" + p.Text) {
				return nil, nil // don't spend a slow model call on unrelated pages
			}
			return LLM(ctx, pl.LLM, p, today)
		})
	}

	for _, stage := range stages {
		events, err := stage()
		if err != nil {
			return nil, err
		}
		events = Upcoming(clean(events), today)
		if len(events) > 0 {
			for i := range events {
				events[i].SourceURL = p.URL
			}
			return events, nil
		}
	}
	return nil, nil
}
