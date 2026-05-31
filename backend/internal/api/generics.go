package api

import (
	"net/http"
	"strings"

	"sub-store/internal/store"
)

type genericCRUD struct {
	db       *store.Store
	key      string
	typeName string
}

func (h *genericCRUD) getAll(c *Context) { success(c, h.db.ReadMapSlice(h.key)) }

func (h *genericCRUD) getOne(c *Context) {
	name := c.Param("name")
	item, _ := store.FindByName(h.db.ReadMapSlice(h.key), name)
	if item == nil {
		failed(c, errNotFound(name, h.typeName), http.StatusNotFound)
		return
	}
	success(c, item)
}

func (h *genericCRUD) create(c *Context) {
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
	list := h.db.ReadMapSlice(h.key)
	if ex, _ := store.FindByName(list, name); ex != nil {
		failed(c, errDuplicate(name), http.StatusBadRequest)
		return
	}
	pos, _ := h.db.ReadMap(store.KeySettings)["createPosition"].(string)
	list = store.InsertByPosition(list, body, pos)
	if err := h.db.Write(h.key, list); err != nil {
		failed(c, errInternal("Failed to save "+h.typeName, err.Error()))
		return
	}
	success(c, body, http.StatusCreated)
}

func (h *genericCRUD) update(c *Context) {
	name := c.Param("name")
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid body", err.Error()), http.StatusBadRequest)
		return
	}
	list := h.db.ReadMapSlice(h.key)
	old, _ := store.FindByName(list, name)
	if old == nil {
		failed(c, errNotFound(name, h.typeName), http.StatusNotFound)
		return
	}
	if _, ok := body["name"]; !ok {
		body["name"] = name
	}
	newItem := mergeMaps(old, body)
	list = store.UpdateByName(list, name, newItem)
	if err := h.db.Write(h.key, list); err != nil {
		failed(c, errInternal("Failed to update "+h.typeName, err.Error()))
		return
	}
	success(c, newItem)
}

func (h *genericCRUD) delete(c *Context) {
	name := c.Param("name")
	list := h.db.ReadMapSlice(h.key)
	if _, idx := store.FindByName(list, name); idx == -1 {
		failed(c, errNotFound(name, h.typeName), http.StatusNotFound)
		return
	}
	if err := h.db.Write(h.key, store.DeleteByName(list, name)); err != nil {
		failed(c, errInternal("Failed to delete "+h.typeName, err.Error()))
		return
	}
	successNoData(c)
}

func (h *genericCRUD) replace(c *Context) {
	var body []map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid body", err.Error()), http.StatusBadRequest)
		return
	}
	_ = h.db.Write(h.key, body)
	successNoData(c)
}

// ---- Concrete handlers ----

type FileHandler struct{ db *store.Store }

func (h *FileHandler) h() *genericCRUD { return &genericCRUD{db: h.db, key: store.KeyFiles, typeName: "File"} }
func (h *FileHandler) getAll(c *Context)  { h.h().getAll(c) }
func (h *FileHandler) getOne(c *Context)  { h.h().getOne(c) }
func (h *FileHandler) create(c *Context)  { h.h().create(c) }
func (h *FileHandler) update(c *Context)  { h.h().update(c) }
func (h *FileHandler) delete(c *Context)  { h.h().delete(c) }
func (h *FileHandler) replace(c *Context) { h.h().replace(c) }

type ModuleHandler struct{ db *store.Store }

func (h *ModuleHandler) h() *genericCRUD { return &genericCRUD{db: h.db, key: store.KeyModules, typeName: "Module"} }
func (h *ModuleHandler) getAll(c *Context)  { h.h().getAll(c) }
func (h *ModuleHandler) getOne(c *Context)  { h.h().getOne(c) }
func (h *ModuleHandler) create(c *Context)  { h.h().create(c) }
func (h *ModuleHandler) update(c *Context)  { h.h().update(c) }
func (h *ModuleHandler) delete(c *Context)  { h.h().delete(c) }
func (h *ModuleHandler) replace(c *Context) { h.h().replace(c) }

type TokenHandler struct{ db *store.Store }

func (h *TokenHandler) h() *genericCRUD { return &genericCRUD{db: h.db, key: store.KeyTokens, typeName: "Token"} }
func (h *TokenHandler) getAll(c *Context)  { h.h().getAll(c) }
func (h *TokenHandler) getOne(c *Context)  { h.h().getOne(c) }
func (h *TokenHandler) create(c *Context)  { h.h().create(c) }
func (h *TokenHandler) update(c *Context)  { h.h().update(c) }
func (h *TokenHandler) delete(c *Context)  { h.h().delete(c) }
func (h *TokenHandler) replace(c *Context) { h.h().replace(c) }

// ---- Token generation helper ----

// TokenHandler extended: token generation
func (h *TokenHandler) generate(c *Context) {
	var body struct {
		Name string  `json:"name"`
		Type string  `json:"type"` // sub | col
		Exp  float64 `json:"exp"`  // expiry Unix ms (0 = never)
	}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid body", err.Error()), 400)
		return
	}
	token := generateToken(32)
	entry := map[string]interface{}{
		"name":      body.Name,
		"type":      body.Type,
		"token":     token,
		"exp":       body.Exp,
		"createdAt": nowMillis(),
	}
	list := h.db.ReadMapSlice(store.KeyTokens)
	list = append(list, entry)
	if err := h.db.Write(store.KeyTokens, list); err != nil {
		failed(c, errInternal("Failed to save token", err.Error()))
		return
	}
	success(c, entry, 201)
}
