package proxy

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"sub-store/internal/store"
)

// cacheEntry stores a cached resource.
type cacheEntry struct {
	Content   string            `json:"content"`
	Headers   map[string]string `json:"headers"`
	CachedAt  int64             `json:"cachedAt"`
	TTL       int64             `json:"ttl"` // seconds
}

// GetCachedContent returns cached content for a URL if not expired.
func GetCachedContent(db *store.Store, cacheKey, rawURL string) (string, map[string]string, bool) {
	var cache map[string]cacheEntry
	if !db.ReadInto(cacheKey, &cache) {
		return "", nil, false
	}
	entry, ok := cache[rawURL]
	if !ok {
		return "", nil, false
	}
	if entry.TTL > 0 {
		age := time.Now().Unix() - entry.CachedAt/1000
		if age > entry.TTL {
			return "", nil, false
		}
	}
	return entry.Content, entry.Headers, true
}

// SetCachedContent stores content for a URL with the given TTL.
func SetCachedContent(db *store.Store, cacheKey, rawURL, content string, headers map[string]string, ttlSec int64) {
	var cache map[string]cacheEntry
	if !db.ReadInto(cacheKey, &cache) {
		cache = make(map[string]cacheEntry)
	}
	cache[rawURL] = cacheEntry{
		Content:  content,
		Headers:  headers,
		CachedAt: time.Now().UnixMilli(),
		TTL:      ttlSec,
	}
	raw, err := json.Marshal(cache)
	if err != nil {
		log.Printf("[cache] marshal error: %v", err)
		return
	}
	if err := db.WriteRaw(cacheKey, raw); err != nil {
		log.Printf("[cache] write error: %v", err)
	}
}

// FetchWithCache fetches content, using cache when available.
func FetchWithCache(db *store.Store, rawURL string, timeout int, proxySetting string) (string, map[string]string, error) {
	settings := db.ReadMap(store.KeySettings)

	// Determine TTL from settings
	ttl := int64(0)
	if v, ok := settings["resourceCacheTtl"].(float64); ok && v > 0 {
		ttl = int64(v)
	}

	if ttl > 0 {
		if content, headers, ok := GetCachedContent(db, store.KeyResourceCache, rawURL); ok {
			log.Printf("[cache] HIT %s", rawURL)
			return content, headers, nil
		}
	}

	content, headers, err := FetchContent(rawURL, timeout, proxySetting)
	if err != nil {
		return "", nil, err
	}

	if ttl > 0 {
		SetCachedContent(db, store.KeyResourceCache, rawURL, content, headers, ttl)
		log.Printf("[cache] SET %s ttl=%ds", rawURL, ttl)
	}

	return content, headers, nil
}

// InvalidateCache removes a specific URL from the resource cache.
func InvalidateCache(db *store.Store, rawURL string) error {
	var cache map[string]cacheEntry
	if !db.ReadInto(store.KeyResourceCache, &cache) {
		return nil
	}
	if _, ok := cache[rawURL]; !ok {
		return nil
	}
	delete(cache, rawURL)
	raw, err := json.Marshal(cache)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return db.WriteRaw(store.KeyResourceCache, raw)
}
