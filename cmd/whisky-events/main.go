// Command whisky-events finds whisky events on web pages.
//
// Extraction uses embedded JSON-LD and text rules; a local LLM (Ollama) is an
// optional last resort enabled with -llm.
//
// Usage:
//
//	whisky-events crawl [-config sites.json] [-cache data/cache.json] [-llm]
//	whisky-events extract -url https://example.jp/event
//	whisky-events extract -file page.html [-llm]
//	whisky-events ping
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/kushinoyuya/whisky-events/internal/crawl"
	"github.com/kushinoyuya/whisky-events/internal/extract"
	"github.com/kushinoyuya/whisky-events/internal/fetch"
	"github.com/kushinoyuya/whisky-events/internal/llm"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "ping":
		err = runPing(os.Args[2:])
	case "extract":
		err = runExtract(os.Args[2:])
	case "crawl":
		err = runCrawl(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  whisky-events crawl   [-config sites.json] [-cache data/cache.json] [-delay 3s] [-site NAME] [-llm ...]
  whisky-events extract (-url URL | -file PATH) [-llm [-ollama URL] [-model NAME]]
  whisky-events ping    [-ollama URL] [-model NAME]`)
}

// llmFlags registers the Ollama connection flags shared by subcommands.
func llmFlags(fs *flag.FlagSet) (baseURL, model *string) {
	baseURL = fs.String("ollama", envOr("OLLAMA_HOST_URL", "http://127.0.0.1:11434"), "Ollama base URL")
	model = fs.String("model", envOr("WHISKY_MODEL", "qwen2.5:3b"), "Ollama model name")
	return
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func runPing(args []string) error {
	fs := flag.NewFlagSet("ping", flag.ExitOnError)
	baseURL, model := llmFlags(fs)
	fs.Parse(args)

	start := time.Now()
	out, err := llm.NewOllama(*baseURL, *model).Generate(context.Background(), `{"ok": true} をそのまま返してください。`)
	if err != nil {
		return err
	}
	fmt.Printf("model %s responded in %s: %s\n", *model, time.Since(start).Round(time.Millisecond), out)
	return nil
}

func runExtract(args []string) error {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	baseURL, model := llmFlags(fs)
	url := fs.String("url", "", "page URL to fetch")
	file := fs.String("file", "", "local HTML file (for offline testing)")
	useLLM := fs.Bool("llm", false, "fall back to the Ollama model when rules find nothing")
	fs.Parse(args)

	ctx := context.Background()
	var html, source string
	switch {
	case *url != "":
		var err error
		if html, err = fetch.New().Get(ctx, *url); err != nil {
			return err
		}
		source = *url
	case *file != "":
		b, err := ioutil.ReadFile(*file)
		if err != nil {
			return err
		}
		html, source = string(b), "file://"+*file
	default:
		return fmt.Errorf("extract: -url or -file is required")
	}

	pl := &extract.Pipeline{}
	if *useLLM {
		pl.LLM = llm.NewOllama(*baseURL, *model)
	}
	start := time.Now()
	events, err := pl.Run(ctx, extract.NewPage(source, html))
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d event(s) in %s\n", len(events), time.Since(start).Round(time.Millisecond))

	return printJSON(events)
}

func runCrawl(args []string) error {
	fs := flag.NewFlagSet("crawl", flag.ExitOnError)
	baseURL, model := llmFlags(fs)
	config := fs.String("config", "sites.json", "site list")
	cachePath := fs.String("cache", "data/cache.json", "extraction cache (created if missing)")
	delay := fs.Duration("delay", 3*time.Second, "minimum gap between requests to one host")
	only := fs.String("site", "", "crawl only sites whose name contains this")
	useLLM := fs.Bool("llm", false, "fall back to the Ollama model when rules find nothing")
	quiet := fs.Bool("q", false, "don't log progress to stderr")
	fs.Parse(args)

	cfg, err := crawl.LoadConfig(*config)
	if err != nil {
		return err
	}
	sites := cfg.Sites
	if *only != "" {
		sites = nil
		for _, s := range cfg.Sites {
			if strings.Contains(s.Name, *only) {
				s.Enabled = true // naming a site explicitly overrides enabled:false
				sites = append(sites, s)
			}
		}
		if len(sites) == 0 {
			return fmt.Errorf("crawl: no site name contains %q", *only)
		}
	}
	cache, err := crawl.LoadCache(*cachePath)
	if err != nil {
		return err
	}

	pl := &extract.Pipeline{}
	if *useLLM {
		pl.LLM = llm.NewOllama(*baseURL, *model)
	}
	c := &crawl.Crawler{
		Fetch:     fetch.New(),
		Pipeline:  pl,
		Cache:     cache,
		UserAgent: fetch.UserAgent,
		Delay:     *delay,
		Log:       os.Stderr,
	}
	if *quiet {
		c.Log = nil
	}

	start := time.Now()
	events, stats := c.Run(context.Background(), sites)
	if err := cache.Save(*cachePath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "%d upcoming event(s) in %s (%s)\n", len(events), time.Since(start).Round(time.Second), stats)
	return printJSON(events)
}

func printJSON(events []extract.Event) error {
	if events == nil {
		events = []extract.Event{}
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(events)
}
