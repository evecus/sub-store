package api

import (
	"encoding/json"
	"net/http"

	"sub-store/internal/store"
)

type SortHandler struct{ db *store.Store }

func (h *SortHandler) sortSubs(c *Context) { h.sortList(c, store.KeySubs) }
func (h *SortHandler) sortCols(c *Context) { h.sortList(c, store.KeyCollections) }

func (h *SortHandler) sortList(c *Context, key string) {
	raw := c.RawBody()
	if len(raw) == 0 {
		failed(c, errInternal("Empty body", ""), http.StatusBadRequest)
		return
	}
	var names []string
	var bodyObj struct {
		Orders []string `json:"orders"`
	}
	if err := json.Unmarshal(raw, &bodyObj); err == nil && len(bodyObj.Orders) > 0 {
		names = bodyObj.Orders
	} else if err := json.Unmarshal(raw, &names); err != nil {
		failed(c, errInternal("Invalid sort body", err.Error()), http.StatusBadRequest)
		return
	}
	list := h.db.ReadMapSlice(key)
	byName := make(map[string]map[string]interface{}, len(list))
	for _, item := range list {
		if n, ok := item["name"].(string); ok {
			byName[n] = item
		}
	}
	sorted := make([]map[string]interface{}, 0, len(list))
	for _, name := range names {
		if item, ok := byName[name]; ok {
			sorted = append(sorted, item)
			delete(byName, name)
		}
	}
	for _, item := range list {
		if n, ok := item["name"].(string); ok {
			if _, still := byName[n]; still {
				sorted = append(sorted, item)
			}
		}
	}
	if err := h.db.Write(key, sorted); err != nil {
		failed(c, errInternal("Failed to save sorted list", err.Error()))
		return
	}
	successNoData(c)
}
