package api

import "sub-store/internal/store"

type NodeInfoHandler struct{ db *store.Store }

func (h *NodeInfoHandler) get(c *Context) {
	success(c, map[string]interface{}{
		"version": "go-backend",
		"env":     "node",
	})
}
