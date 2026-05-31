package api

import (
	"net/http"
	"strings"
	"time"

	"sub-store/internal/store"
)

type SubHandler struct{ db *store.Store }

func (h *SubHandler) getAll(c *Context) {
	success(c, h.db.ReadMapSlice(store.KeySubs))
}

func (h *SubHandler) getOne(c *Context) {
	name := c.Param("name")
	item, _ := store.FindByName(h.db.ReadMapSlice(store.KeySubs), name)
	if item == nil {
		failed(c, errNotFound(name, "Subscription"), http.StatusNotFound)
		return
	}
	delete(item, "subscriptions")
	if c.HasQuery("raw") {
		fname := "sub-store_subscription_" + name + "_" + formatDateTime(time.Now()) + ".json"
		c.SetHeader("Content-Disposition", `attachment; filename="`+fname+`"`)
	}
	success(c, item)
}

func (h *SubHandler) create(c *Context) {
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	delete(body, "subscriptions")
	name, _ := body["name"].(string)
	if strings.Contains(name, "/") {
		failed(c, errInvalidName(name), http.StatusBadRequest)
		return
	}
	list := h.db.ReadMapSlice(store.KeySubs)
	if ex, _ := store.FindByName(list, name); ex != nil {
		failed(c, errDuplicate(name), http.StatusBadRequest)
		return
	}
	pos, _ := h.db.ReadMap(store.KeySettings)["createPosition"].(string)
	list = store.InsertByPosition(list, body, pos)
	if err := h.db.Write(store.KeySubs, list); err != nil {
		failed(c, errInternal("Failed to save", err.Error()))
		return
	}
	success(c, body, http.StatusCreated)
}

func (h *SubHandler) update(c *Context) {
	name := c.Param("name")
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	delete(body, "subscriptions")
	list := h.db.ReadMapSlice(store.KeySubs)
	old, _ := store.FindByName(list, name)
	if old == nil {
		failed(c, errNotFound(name, "Subscription"), http.StatusNotFound)
		return
	}
	if _, ok := body["name"]; !ok {
		body["name"] = name
	}
	newItem := mergeMaps(old, body)
	newName, _ := newItem["name"].(string)
	if name != newName {
		h.renameInCollections(name, newName)
		h.renameInArtifacts(name, newName, "subscription")
		h.renameInFiles(name, newName, "subscription")
	}
	list = store.UpdateByName(list, name, newItem)
	if err := h.db.Write(store.KeySubs, list); err != nil {
		failed(c, errInternal("Failed to update", err.Error()))
		return
	}
	success(c, newItem)
}

func (h *SubHandler) delete(c *Context) {
	name := c.Param("name")
	mode := c.Query("mode")
	list := h.db.ReadMapSlice(store.KeySubs)
	item, _ := store.FindByName(list, name)
	if item == nil {
		failed(c, errNotFound(name, "Subscription"), http.StatusNotFound)
		return
	}
	if mode == "archive" {
		archives := h.db.ReadMapSlice(store.KeyArchives)
		entry := copyMap(item)
		entry["archivedAt"] = time.Now().UnixMilli()
		entry["type"] = "subscription"
		_ = h.db.Write(store.KeyArchives, append(archives, entry))
	} else if mode != "" && mode != "permanent" {
		failed(c, APIError{Code: "INVALID_DELETE_MODE", Type: "RequestInvalidError",
			Message: "Unsupported delete mode: " + mode}, http.StatusBadRequest)
		return
	}
	list = store.DeleteByName(list, name)
	if err := h.db.Write(store.KeySubs, list); err != nil {
		failed(c, errInternal("Failed to delete", err.Error()))
		return
	}
	// Remove from collections
	cols := h.db.ReadMapSlice(store.KeyCollections)
	for _, col := range cols {
		if subs, ok := col["subscriptions"].([]interface{}); ok {
			filtered := make([]interface{}, 0)
			for _, s := range subs {
				if sn, ok := s.(string); ok && sn != name {
					filtered = append(filtered, s)
				}
			}
			col["subscriptions"] = filtered
		}
	}
	_ = h.db.Write(store.KeyCollections, cols)
	successNoData(c)
}

func (h *SubHandler) replace(c *Context) {
	var body []map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	if err := h.db.Write(store.KeySubs, body); err != nil {
		failed(c, errInternal("Failed to replace", err.Error()))
		return
	}
	successNoData(c)
}

func (h *SubHandler) getFlowInfo(c *Context) {
	name := c.Param("name")
	item, _ := store.FindByName(h.db.ReadMapSlice(store.KeySubs), name)
	if item == nil {
		failed(c, errNotFound(name, "Subscription"), http.StatusNotFound)
		return
	}
	subURL, _ := item["url"].(string)
	if qURL := c.Query("url"); qURL != "" {
		subURL = qURL
	}
	source, _ := item["source"].(string)
	subUserinfo, _ := item["subUserinfo"].(string)
	if source == "local" {
		if subUserinfo != "" && !strings.HasPrefix(subUserinfo, "http") {
			parsed := parseFlowHeaders(subUserinfo)
			if parsed == nil {
				failed(c, errNoFlow(), http.StatusBadRequest)
				return
			}
			success(c, parsed)
			return
		}
		failed(c, errNoFlow(), http.StatusBadRequest)
		return
	}
	firstURL := firstLine(subURL)
	baseURL, arguments := parseURLArguments(firstURL)
	noFlow, _ := arguments["noFlow"].(bool)
	if noFlow || !strings.HasPrefix(baseURL, "http") {
		failed(c, errNoFlow(), http.StatusBadRequest)
		return
	}
	flowURL := baseURL
	if insecure, _ := arguments["insecure"].(bool); insecure {
		flowURL = baseURL + "#insecure"
	}
	if customFlowURL, ok := arguments["flowUrl"].(string); ok && customFlowURL != "" {
		flowURL = customFlowURL
	}
	proxy, _ := item["proxy"].(string)
	headers, err := fetchFlowHeaders(flowURL, proxy)
	if err != nil {
		failed(c, errNetwork("Failed to fetch flow headers: "+err.Error()))
		return
	}
	parsed := parseFlowHeaders(headers)
	if parsed == nil {
		failed(c, errNoFlow(), http.StatusBadRequest)
		return
	}
	success(c, parsed)
}

func (h *SubHandler) renameInCollections(oldName, newName string) {
	cols := h.db.ReadMapSlice(store.KeyCollections)
	for _, col := range cols {
		if subs, ok := col["subscriptions"].([]interface{}); ok {
			for i, s := range subs {
				if sn, ok := s.(string); ok && sn == oldName {
					subs[i] = newName
				}
			}
		}
	}
	_ = h.db.Write(store.KeyCollections, cols)
}

func (h *SubHandler) renameInArtifacts(oldName, newName, typ string) {
	arts := h.db.ReadMapSlice(store.KeyArtifacts)
	for _, a := range arts {
		if t, _ := a["type"].(string); t == typ {
			if src, _ := a["source"].(string); src == oldName {
				a["source"] = newName
			}
		}
	}
	_ = h.db.Write(store.KeyArtifacts, arts)
}

func (h *SubHandler) renameInFiles(oldName, newName, typ string) {
	files := h.db.ReadMapSlice(store.KeyFiles)
	for _, f := range files {
		if st, _ := f["sourceType"].(string); st == typ {
			if sn, _ := f["sourceName"].(string); sn == oldName {
				f["sourceName"] = newName
			}
		}
	}
	_ = h.db.Write(store.KeyFiles, files)
}
