package api

import (
	"net/http"
	"strings"
	"time"

	"sub-store/internal/store"
)

type ColHandler struct{ db *store.Store }

func (h *ColHandler) getAll(c *Context) {
	success(c, h.db.ReadMapSlice(store.KeyCollections))
}

func (h *ColHandler) getOne(c *Context) {
	name := c.Param("name")
	item, _ := store.FindByName(h.db.ReadMapSlice(store.KeyCollections), name)
	if item == nil {
		failed(c, errNotFound(name, "Collection"), http.StatusNotFound)
		return
	}
	if c.HasQuery("raw") {
		fname := "sub-store_collection_" + name + "_" + formatDateTime(time.Now()) + ".json"
		c.SetHeader("Content-Disposition", `attachment; filename="`+fname+`"`)
	}
	success(c, item)
}

func (h *ColHandler) create(c *Context) {
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	name, _ := body["name"].(string)
	if strings.Contains(name, "/") {
		failed(c, errInvalidName(name), http.StatusBadRequest)
		return
	}
	list := h.db.ReadMapSlice(store.KeyCollections)
	if ex, _ := store.FindByName(list, name); ex != nil {
		failed(c, errDuplicate(name), http.StatusBadRequest)
		return
	}
	pos, _ := h.db.ReadMap(store.KeySettings)["createPosition"].(string)
	list = store.InsertByPosition(list, body, pos)
	if err := h.db.Write(store.KeyCollections, list); err != nil {
		failed(c, errInternal("Failed to save", err.Error()))
		return
	}
	success(c, body, http.StatusCreated)
}

func (h *ColHandler) update(c *Context) {
	name := c.Param("name")
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	list := h.db.ReadMapSlice(store.KeyCollections)
	old, _ := store.FindByName(list, name)
	if old == nil {
		failed(c, errNotFound(name, "Collection"), http.StatusNotFound)
		return
	}
	if _, ok := body["name"]; !ok {
		body["name"] = name
	}
	newItem := mergeMaps(old, body)
	newName, _ := newItem["name"].(string)
	if name != newName {
		arts := h.db.ReadMapSlice(store.KeyArtifacts)
		for _, a := range arts {
			if t, _ := a["type"].(string); t == "collection" {
				if src, _ := a["source"].(string); src == name {
					a["source"] = newName
				}
			}
		}
		_ = h.db.Write(store.KeyArtifacts, arts)
		files := h.db.ReadMapSlice(store.KeyFiles)
		for _, f := range files {
			if st, _ := f["sourceType"].(string); st == "collection" {
				if sn, _ := f["sourceName"].(string); sn == name {
					f["sourceName"] = newName
				}
			}
		}
		_ = h.db.Write(store.KeyFiles, files)
	}
	list = store.UpdateByName(list, name, newItem)
	if err := h.db.Write(store.KeyCollections, list); err != nil {
		failed(c, errInternal("Failed to update", err.Error()))
		return
	}
	success(c, newItem)
}

func (h *ColHandler) delete(c *Context) {
	name := c.Param("name")
	mode := c.Query("mode")
	list := h.db.ReadMapSlice(store.KeyCollections)
	item, _ := store.FindByName(list, name)
	if item == nil {
		failed(c, errNotFound(name, "Collection"), http.StatusNotFound)
		return
	}
	if mode == "archive" {
		archives := h.db.ReadMapSlice(store.KeyArchives)
		entry := copyMap(item)
		entry["archivedAt"] = time.Now().UnixMilli()
		entry["type"] = "collection"
		_ = h.db.Write(store.KeyArchives, append(archives, entry))
	} else if mode != "" && mode != "permanent" {
		failed(c, APIError{Code: "INVALID_DELETE_MODE", Type: "RequestInvalidError",
			Message: "Unsupported delete mode: " + mode}, http.StatusBadRequest)
		return
	}
	list = store.DeleteByName(list, name)
	if err := h.db.Write(store.KeyCollections, list); err != nil {
		failed(c, errInternal("Failed to delete", err.Error()))
		return
	}
	successNoData(c)
}

func (h *ColHandler) replace(c *Context) {
	var body []map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	if err := h.db.Write(store.KeyCollections, body); err != nil {
		failed(c, errInternal("Failed to replace", err.Error()))
		return
	}
	successNoData(c)
}
