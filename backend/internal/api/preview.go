package api

import (
	"net/http"

	"sub-store/internal/proxy"
	"sub-store/internal/store"
)

type PreviewHandler struct{ db *store.Store }

func (h *PreviewHandler) previewSub(c *Context) {
	name := c.Param("name")
	sub, _ := store.FindByName(h.db.ReadMapSlice(store.KeySubs), name)
	if sub == nil {
		failed(c, errNotFound(name, "Subscription"), http.StatusNotFound)
		return
	}
	target := c.DefaultQuery("target", "JSON")
	proxies, err := proxy.FetchAndParse(sub, h.db)
	if err != nil {
		failed(c, errInternal("Failed to fetch subscription", err.Error()))
		return
	}
	output, err := proxy.Produce(proxies, target, sub)
	if err != nil {
		failed(c, errInternal("Failed to produce output", err.Error()))
		return
	}
	success(c, map[string]interface{}{"output": output, "proxies": proxies, "target": target})
}

func (h *PreviewHandler) previewCol(c *Context) {
	name := c.Param("name")
	col, _ := store.FindByName(h.db.ReadMapSlice(store.KeyCollections), name)
	if col == nil {
		failed(c, errNotFound(name, "Collection"), http.StatusNotFound)
		return
	}
	target := c.DefaultQuery("target", "JSON")
	proxies, err := proxy.FetchAndParseCollection(col, h.db)
	if err != nil {
		failed(c, errInternal("Failed to fetch collection", err.Error()))
		return
	}
	output, err := proxy.Produce(proxies, target, col)
	if err != nil {
		failed(c, errInternal("Failed to produce output", err.Error()))
		return
	}
	success(c, map[string]interface{}{"output": output, "proxies": proxies, "target": target})
}
