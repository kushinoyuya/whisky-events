// Package fetch downloads pages politely: identifying user agent, timeout,
// and a size cap.
package fetch

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"time"
	"unicode/utf8"
)

const maxBytes = 2 << 20 // 2 MiB is plenty for an event page

// UserAgent identifies the crawler to site operators.
const UserAgent = "whisky-events/0.1 (+https://github.com/kushinoyuya/whisky-events)"

// Client fetches pages over HTTP.
type Client struct {
	HTTP *http.Client
}

// New returns a Client with a 30s timeout.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// Get returns the body of url as a string. Non-UTF-8 pages (e.g. Shift_JIS)
// are rejected for now, since decoding them needs golang.org/x/text.
func (c *Client) Get(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept-Language", "ja,en;q=0.5")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	body, err := ioutil.ReadAll(io.LimitReader(resp.Body, maxBytes))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(body) {
		return "", fmt.Errorf("GET %s: body is not UTF-8 (Shift_JIS/EUC-JP not supported yet)", url)
	}
	return string(body), nil
}
