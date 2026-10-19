package crawl

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/kushinoyuya/whisky-events/internal/extract"
)

// Cache remembers what each page contained last time, so unchanged pages
// skip extraction (and above all, a slow LLM call).
type Cache struct {
	Pages map[string]CacheEntry `json:"pages"`
}

// CacheEntry is the last extraction result for one URL.
type CacheEntry struct {
	Hash      string          `json:"hash"`
	CheckedAt time.Time       `json:"checked_at"`
	Events    []extract.Event `json:"events"`
}

// LoadCache reads path, returning an empty cache if it doesn't exist yet.
func LoadCache(path string) (*Cache, error) {
	c := &Cache{Pages: map[string]CacheEntry{}}
	b, err := ioutil.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, err
	}
	if c.Pages == nil {
		c.Pages = map[string]CacheEntry{}
	}
	return c, nil
}

// Save writes the cache atomically (temp file + rename) so an interrupted
// crawl never leaves a truncated cache behind.
func (c *Cache) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := ioutil.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func contentHash(page string) string {
	sum := sha256.Sum256([]byte(page))
	return hex.EncodeToString(sum[:])
}
