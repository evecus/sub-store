package api

import (
	"log"

	"sub-store/internal/proxy"
	"sub-store/internal/store"
)

// CacheHandler handles cache invalidation routes.
type CacheHandler struct {
	db *store.Store
}

func (h *CacheHandler) invalidateSub(c *Context) {
	name := c.Param("name")
	subs := h.db.ReadMapSlice(store.KeySubs)
	sub, _ := store.FindByName(subs, name)
	if sub == nil {
		failed(c, errNotFound(name, "Subscription"), 404)
		return
	}
	rawURL, _ := sub["url"].(string)
	rawURL = firstLine(rawURL)
	if rawURL != "" {
		if err := proxy.InvalidateCache(h.db, rawURL); err != nil {
			log.Printf("[cache] invalidate sub %s: %v", name, err)
		}
	}
	log.Printf("[cache] invalidated sub: %s", name)
	successNoData(c)
}

func (h *CacheHandler) invalidateCol(c *Context) {
	name := c.Param("name")
	cols := h.db.ReadMapSlice(store.KeyCollections)
	col, _ := store.FindByName(cols, name)
	if col == nil {
		failed(c, errNotFound(name, "Collection"), 404)
		return
	}
	subsRaw, _ := col["subscriptions"].([]interface{})
	allSubs := h.db.ReadMapSlice(store.KeySubs)
	for _, nameRaw := range subsRaw {
		subName, _ := nameRaw.(string)
		if subName == "" {
			continue
		}
		sub, _ := store.FindByName(allSubs, subName)
		if sub == nil {
			continue
		}
		rawURL, _ := sub["url"].(string)
		rawURL = firstLine(rawURL)
		if rawURL != "" {
			_ = proxy.InvalidateCache(h.db, rawURL)
		}
	}
	log.Printf("[cache] invalidated collection: %s", name)
	successNoData(c)
}

func (h *CacheHandler) invalidateAll(c *Context) {
	_ = h.db.Delete(store.KeyResourceCache)
	_ = h.db.Delete(store.KeyHeadersResourceCache)
	_ = h.db.Delete(store.KeyScriptResourceCache)
	log.Println("[cache] all caches cleared")
	successNoData(c)
}
