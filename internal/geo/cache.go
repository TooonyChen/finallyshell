package geo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CacheTTL is how long an online answer is kept.
const CacheTTL = 30 * 24 * time.Hour

// Cache keeps what the online services said of each address, in a file
// (cache.json in the data folder), for CacheTTL: an address is looked up
// online once a month at most. It is safe for concurrent use.
type Cache struct {
	path string

	mu      sync.Mutex
	entries map[string]cacheEntry // by lang + " " + IP
	changed bool
}

type cacheEntry struct {
	Location
	At time.Time `json:"at"`
}

// LoadCache reads the cache at path, leaving out what is older than
// CacheTTL: a missing or damaged file is an empty cache.
func LoadCache(path string) *Cache {
	c := &Cache{path: path, entries: map[string]cacheEntry{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	var file struct {
		Entries map[string]cacheEntry `json:"entries"`
	}
	if json.Unmarshal(data, &file) != nil {
		return c
	}
	now := time.Now()
	for k, e := range file.Entries {
		if now.Sub(e.At) < CacheTTL {
			c.entries[k] = e
		}
	}
	return c
}

func cacheKey(lang, ip string) string { return lang + " " + ip }

// Get is what the services said of ip, in lang, unless it is older than
// CacheTTL. An address they know nothing of is known, with an empty
// Location.
func (c *Cache) Get(lang, ip string) (Location, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[cacheKey(lang, ip)]
	if !ok || time.Since(e.At) >= CacheTTL {
		return Location{}, false
	}
	return e.Location, true
}

// Put keeps what the services said of ip, in lang.
func (c *Cache) Put(lang, ip string, loc Location) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[cacheKey(lang, ip)] = cacheEntry{Location: loc, At: time.Now()}
	c.changed = true
}

// Save writes the cache to its file when it changed since it was read
// or last saved.
func (c *Cache) Save() error {
	c.mu.Lock()
	if !c.changed {
		c.mu.Unlock()
		return nil
	}
	data, err := json.Marshal(struct {
		Entries map[string]cacheEntry `json:"entries"`
	}{c.entries})
	c.changed = false
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}
