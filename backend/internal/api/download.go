package api

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"sub-store/internal/proxy"
	"sub-store/internal/store"
)

type DownloadHandler struct {
	db      *store.Store
	version string
}

func (h *DownloadHandler) downloadSubWithTarget(c *Context) {
	// target comes from URL path param
	h.downloadSub(c)
}

func (h *DownloadHandler) downloadSub(c *Context) {
	name := c.Param("name")
	if strings.HasPrefix(c.FullPath(), "/share/") {
		if !h.validateToken(c, "sub", name) {
			settings := h.db.ReadMap(store.KeySettings)
			appearance, _ := settings["appearanceSetting"].(map[string]interface{})
			if appearance != nil {
				if fakeNode, _ := appearance["invalidShareFakeNode"].(bool); fakeNode {
					// serve fake empty sub
					name = "__fake__"
				} else {
					c.Status(http.StatusForbidden)
					return
				}
			} else {
				c.Status(http.StatusForbidden)
				return
			}
		}
	}
	subs := h.db.ReadMapSlice(store.KeySubs)
	sub, _ := store.FindByName(subs, name)
	if sub == nil {
		c.Status(http.StatusNotFound)
		return
	}
	target := h.resolveTarget(c, sub)
	log.Printf("[download] sub=%s target=%s ua=%s", name, target, c.Header("User-Agent"))
	proxies, err := proxy.FetchAndParse(sub, h.db)
	if err != nil {
		log.Printf("[download] fetch error: %v", err)
		c.String(http.StatusInternalServerError, "Failed to fetch subscription: "+err.Error())
		return
	}
	output, contentType, err := proxy.ProduceWithContentType(proxies, target, sub)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to produce output: "+err.Error())
		return
	}
	h.setFlowHeaders(c, sub)
	c.Data(http.StatusOK, contentType, []byte(output))
}

func (h *DownloadHandler) downloadColWithTarget(c *Context) {
	h.downloadCol(c)
}

func (h *DownloadHandler) downloadCol(c *Context) {
	name := c.Param("name")
	if strings.HasPrefix(c.FullPath(), "/share/") {
		if !h.validateToken(c, "col", name) {
			c.Status(http.StatusForbidden)
			return
		}
	}
	cols := h.db.ReadMapSlice(store.KeyCollections)
	col, _ := store.FindByName(cols, name)
	if col == nil {
		c.Status(http.StatusNotFound)
		return
	}
	target := h.resolveTarget(c, col)
	log.Printf("[download] col=%s target=%s", name, target)
	proxies, err := proxy.FetchAndParseCollection(col, h.db)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to fetch collection: "+err.Error())
		return
	}
	output, contentType, err := proxy.ProduceWithContentType(proxies, target, col)
	if err != nil {
		c.String(http.StatusInternalServerError, "Failed to produce output: "+err.Error())
		return
	}
	h.setFlowHeaders(c, col)
	c.Data(http.StatusOK, contentType, []byte(output))
}

func (h *DownloadHandler) nezhaServerDetails(c *Context) {
	// Inject query params for nezha format
	q := c.R.URL.Query()
	q.Set("platform", "JSON")
	q.Set("produceType", "internal")
	q.Set("resultFormat", "nezha")
	c.R.URL.RawQuery = q.Encode()
	h.downloadSub(c)
}

func (h *DownloadHandler) nezhaServerDetailsCol(c *Context) {
	q := c.R.URL.Query()
	q.Set("platform", "JSON")
	q.Set("produceType", "internal")
	q.Set("resultFormat", "nezha")
	c.R.URL.RawQuery = q.Encode()
	h.downloadCol(c)
}

func (h *DownloadHandler) resolveTarget(c *Context, item map[string]interface{}) string {
	if t := c.Param("target"); t != "" {
		return t
	}
	if t := c.Query("target"); t != "" {
		return t
	}
	if t := c.Query("platform"); t != "" {
		return t
	}
	return detectTargetFromUA(c.Header("User-Agent"))
}

func detectTargetFromUA(ua string) string {
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "quantumult x"):
		return "QX"
	case strings.Contains(ua, "quantumult"):
		return "QX"
	case strings.Contains(ua, "loon"):
		return "Loon"
	case strings.Contains(ua, "surge"):
		return "Surge"
	case strings.Contains(ua, "shadowrocket"):
		return "Shadowrocket"
	case strings.Contains(ua, "clash"):
		return "ClashMeta"
	case strings.Contains(ua, "sing-box"):
		return "SingBox"
	case strings.Contains(ua, "stash"):
		return "Stash"
	default:
		return "ClashMeta"
	}
}

func (h *DownloadHandler) validateToken(c *Context, typ, name string) bool {
	token := c.Query("token")
	if token == "" {
		return false
	}
	tokens := h.db.ReadMapSlice(store.KeyTokens)
	now := nowMillis()
	for _, t := range tokens {
		if t["token"] == token && t["type"] == typ && t["name"] == name {
			exp, _ := t["exp"].(float64)
			if exp == 0 || int64(exp) > now {
				return true
			}
		}
	}
	return false
}

func (h *DownloadHandler) setFlowHeaders(c *Context, item map[string]interface{}) {
	if info, ok := item["_flowInfo"].(map[string]interface{}); ok {
		if v, ok := info["subscription-userinfo"].(string); ok {
			c.SetHeader("subscription-userinfo", v)
		}
	}
}

func (h *DownloadHandler) notFoundOrFake(c *Context, name string) {
	settings := h.db.ReadMap(store.KeySettings)
	appearance, _ := settings["appearanceSetting"].(map[string]interface{})
	if appearance != nil {
		if fakeNode, _ := appearance["invalidShareFakeNode"].(bool); fakeNode {
			c.String(http.StatusOK, fmt.Sprintf("# Node not found: %s\n", name))
			return
		}
	}
	c.Status(http.StatusNotFound)
}
