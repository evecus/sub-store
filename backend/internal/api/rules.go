package api

import "sub-store/internal/store"

// RuleHandler handles /api/rule* routes.
type RuleHandler struct{ db *store.Store }

func (h *RuleHandler) h() *genericCRUD {
	return &genericCRUD{db: h.db, key: store.KeyRules, typeName: "Rule"}
}

func (h *RuleHandler) getAll(c *Context)  { h.h().getAll(c) }
func (h *RuleHandler) getOne(c *Context)  { h.h().getOne(c) }
func (h *RuleHandler) create(c *Context)  { h.h().create(c) }
func (h *RuleHandler) update(c *Context)  { h.h().update(c) }
func (h *RuleHandler) delete(c *Context)  { h.h().delete(c) }
func (h *RuleHandler) replace(c *Context) { h.h().replace(c) }
