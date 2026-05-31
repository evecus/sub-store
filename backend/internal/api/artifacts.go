package api

import (
	"net/http"
	"strings"

	"sub-store/internal/store"
)

type ArtifactHandler struct{ db *store.Store }

func (h *ArtifactHandler) getAll(c *Context) { success(c, h.db.ReadMapSlice(store.KeyArtifacts)) }

func (h *ArtifactHandler) getOne(c *Context) {
	name := c.Param("name")
	item, _ := store.FindByName(h.db.ReadMapSlice(store.KeyArtifacts), name)
	if item == nil {
		failed(c, errNotFound(name, "Artifact"), http.StatusNotFound)
		return
	}
	success(c, item)
}

func (h *ArtifactHandler) create(c *Context) {
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid body", err.Error()), http.StatusBadRequest)
		return
	}
	name, _ := body["name"].(string)
	if strings.Contains(name, "/") {
		failed(c, errInvalidName(name), http.StatusBadRequest)
		return
	}
	list := h.db.ReadMapSlice(store.KeyArtifacts)
	if ex, _ := store.FindByName(list, name); ex != nil {
		failed(c, errDuplicate(name), http.StatusBadRequest)
		return
	}
	pos, _ := h.db.ReadMap(store.KeySettings)["createPosition"].(string)
	list = store.InsertByPosition(list, body, pos)
	if err := h.db.Write(store.KeyArtifacts, list); err != nil {
		failed(c, errInternal("Failed to save", err.Error()))
		return
	}
	success(c, body, http.StatusCreated)
}

func (h *ArtifactHandler) update(c *Context) {
	name := c.Param("name")
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid body", err.Error()), http.StatusBadRequest)
		return
	}
	list := h.db.ReadMapSlice(store.KeyArtifacts)
	old, _ := store.FindByName(list, name)
	if old == nil {
		failed(c, errNotFound(name, "Artifact"), http.StatusNotFound)
		return
	}
	if _, ok := body["name"]; !ok {
		body["name"] = name
	}
	newItem := mergeMaps(old, body)
	list = store.UpdateByName(list, name, newItem)
	if err := h.db.Write(store.KeyArtifacts, list); err != nil {
		failed(c, errInternal("Failed to update", err.Error()))
		return
	}
	success(c, newItem)
}

func (h *ArtifactHandler) delete(c *Context) {
	name := c.Param("name")
	list := h.db.ReadMapSlice(store.KeyArtifacts)
	if _, idx := store.FindByName(list, name); idx == -1 {
		failed(c, errNotFound(name, "Artifact"), http.StatusNotFound)
		return
	}
	if err := h.db.Write(store.KeyArtifacts, store.DeleteByName(list, name)); err != nil {
		failed(c, errInternal("Failed to delete", err.Error()))
		return
	}
	successNoData(c)
}

func (h *ArtifactHandler) replace(c *Context) {
	var body []map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid body", err.Error()), http.StatusBadRequest)
		return
	}
	_ = h.db.Write(store.KeyArtifacts, body)
	successNoData(c)
}

func (h *ArtifactHandler) syncAll(c *Context) {
	success(c, map[string]string{"message": "sync initiated"})
}

func (h *ArtifactHandler) syncOne(c *Context) {
	name := c.Param("name")
	item, _ := store.FindByName(h.db.ReadMapSlice(store.KeyArtifacts), name)
	if item == nil {
		failed(c, errNotFound(name, "Artifact"), http.StatusNotFound)
		return
	}
	success(c, map[string]string{"message": "sync initiated", "artifact": name})
}
