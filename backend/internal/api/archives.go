package api

import (
	"net/http"

	"sub-store/internal/store"
)

type ArchiveHandler struct{ db *store.Store }

func (h *ArchiveHandler) getAll(c *Context) { success(c, h.db.ReadMapSlice(store.KeyArchives)) }

func (h *ArchiveHandler) delete(c *Context) {
	name := c.Param("name")
	list := h.db.ReadMapSlice(store.KeyArchives)
	newList := make([]map[string]interface{}, 0, len(list))
	found := false
	for _, item := range list {
		if n, _ := item["name"].(string); n == name {
			found = true
			continue
		}
		newList = append(newList, item)
	}
	if !found {
		failed(c, errNotFound(name, "Archive"), http.StatusNotFound)
		return
	}
	_ = h.db.Write(store.KeyArchives, newList)
	successNoData(c)
}

func (h *ArchiveHandler) restore(c *Context) {
	name := c.Param("name")
	archives := h.db.ReadMapSlice(store.KeyArchives)
	var item map[string]interface{}
	for _, a := range archives {
		if n, _ := a["name"].(string); n == name {
			item = a
			break
		}
	}
	if item == nil {
		failed(c, errNotFound(name, "Archive"), http.StatusNotFound)
		return
	}
	typ, _ := item["type"].(string)
	restored := copyMap(item)
	delete(restored, "type")
	delete(restored, "archivedAt")
	switch typ {
	case "subscription":
		list := h.db.ReadMapSlice(store.KeySubs)
		if ex, _ := store.FindByName(list, name); ex != nil {
			failed(c, errDuplicate(name), http.StatusBadRequest)
			return
		}
		_ = h.db.Write(store.KeySubs, append(list, restored))
	case "collection":
		list := h.db.ReadMapSlice(store.KeyCollections)
		if ex, _ := store.FindByName(list, name); ex != nil {
			failed(c, errDuplicate(name), http.StatusBadRequest)
			return
		}
		_ = h.db.Write(store.KeyCollections, append(list, restored))
	default:
		failed(c, errInternal("Unknown archive type: "+typ, ""), http.StatusBadRequest)
		return
	}
	newArchives := make([]map[string]interface{}, 0, len(archives)-1)
	for _, a := range archives {
		if n, _ := a["name"].(string); n != name {
			newArchives = append(newArchives, a)
		}
	}
	_ = h.db.Write(store.KeyArchives, newArchives)
	success(c, restored)
}
