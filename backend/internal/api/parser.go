package api

import (
	"net/http"
	"sub-store/internal/proxy"
)

type ParserHandler struct{}

func (h *ParserHandler) parse(c *Context) {
	var body struct {
		Content string `json:"content"`
		Format  string `json:"format"`
	}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	proxies, err := proxy.ParseContent(body.Content, body.Format)
	if err != nil {
		failed(c, errInternal("Parse failed", err.Error()))
		return
	}
	success(c, proxies)
}
