package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Router is a minimal HTTP router supporting path parameters (:name), CORS, and method routing.
type Router struct {
	routes   []route
	fallback http.HandlerFunc // handles paths with no matching route (used for SPA static files)
}

type route struct {
	method  string
	pattern []string // split by "/"
	handler http.HandlerFunc
}

// Context carries path parameters extracted during routing.
type Context struct {
	Params map[string]string
	W      http.ResponseWriter
	R      *http.Request
}

// HandlerFunc is a handler that receives a Context.
type HandlerFunc func(*Context)

func NewRouter() *Router { return &Router{} }

// SetFallback registers a catch-all handler invoked when no route matches.
// Used by registerStaticFrontend to serve the embedded Vue SPA.
func (r *Router) SetFallback(h http.HandlerFunc) {
	r.fallback = h
}

func (r *Router) add(method, path string, h HandlerFunc) {
	r.routes = append(r.routes, route{
		method:  method,
		pattern: splitPath(path),
		handler: func(w http.ResponseWriter, req *http.Request) {
			params := make(map[string]string)
			match(splitPath(req.URL.Path), splitPath(path), params)
			h(&Context{Params: params, W: w, R: req})
		},
	})
}

func (r *Router) GET(path string, h HandlerFunc)    { r.add("GET", path, h) }
func (r *Router) POST(path string, h HandlerFunc)   { r.add("POST", path, h) }
func (r *Router) PUT(path string, h HandlerFunc)    { r.add("PUT", path, h) }
func (r *Router) PATCH(path string, h HandlerFunc)  { r.add("PATCH", path, h) }
func (r *Router) DELETE(path string, h HandlerFunc) { r.add("DELETE", path, h) }

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Access-Control-Expose-Headers", "*")

	if req.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	reqParts := splitPath(req.URL.Path)

	// Find best matching route (longest specific match wins)
	var bestHandler http.HandlerFunc
	bestScore := -1

	for _, rt := range r.routes {
		if rt.method != req.Method {
			continue
		}
		params := make(map[string]string)
		score := matchScore(reqParts, rt.pattern, params)
		if score > bestScore {
			bestScore = score
			bestHandler = rt.handler
		}
	}

	if bestHandler != nil {
		bestHandler(w, req)
		return
	}

	// No API route matched — delegate to fallback (static SPA) if registered
	if r.fallback != nil {
		r.fallback(w, req)
		return
	}

	http.NotFound(w, req)
}

// splitPath splits a URL path into segments, ignoring empty parts.
func splitPath(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	result := make([]string, 0, len(parts))
	for _, s := range parts {
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

// matchScore returns a score >= 0 if the path matches the pattern, -1 otherwise.
// Higher scores mean more specific (literal) matches.
func matchScore(path, pattern []string, params map[string]string) int {
	if len(path) != len(pattern) {
		return -1
	}
	score := 0
	for i, seg := range pattern {
		if strings.HasPrefix(seg, ":") {
			params[seg[1:]] = path[i]
		} else if seg == path[i] {
			score++
		} else {
			return -1
		}
	}
	return score
}

// match fills params map from path against pattern (used by handler wrapper).
func match(path, pattern []string, params map[string]string) {
	for i, seg := range pattern {
		if i >= len(path) {
			break
		}
		if strings.HasPrefix(seg, ":") {
			params[seg[1:]] = path[i]
		}
	}
}

// ---- Context helpers ----

// Param returns the named path parameter.
func (c *Context) Param(name string) string {
	return c.Params[name]
}

// Query returns the named query parameter.
func (c *Context) Query(name string) string {
	return c.R.URL.Query().Get(name)
}

// DefaultQuery returns the named query param, or def if absent.
func (c *Context) DefaultQuery(name, def string) string {
	v := c.R.URL.Query().Get(name)
	if v == "" {
		return def
	}
	return v
}

// HasQuery returns true if the query parameter exists (even if empty).
func (c *Context) HasQuery(name string) bool {
	_, ok := c.R.URL.Query()[name]
	return ok
}

// Header returns a request header value.
func (c *Context) Header(name string) string {
	return c.R.Header.Get(name)
}

// SetHeader sets a response header.
func (c *Context) SetHeader(name, value string) {
	c.W.Header().Set(name, value)
}

// BindJSON decodes the request body as JSON into dst.
func (c *Context) BindJSON(dst interface{}) error {
	defer c.R.Body.Close()
	return json.NewDecoder(c.R.Body).Decode(dst)
}

// RawBody reads and returns the raw request body bytes.
func (c *Context) RawBody() []byte {
	defer c.R.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := c.R.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return buf
}

// JSON writes a JSON response.
func (c *Context) JSON(code int, v interface{}) {
	c.W.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.W.WriteHeader(code)
	_ = json.NewEncoder(c.W).Encode(v)
}

// Data writes raw bytes with a content type.
func (c *Context) Data(code int, contentType string, data []byte) {
	c.W.Header().Set("Content-Type", contentType)
	c.W.WriteHeader(code)
	_, _ = c.W.Write(data)
}

// String writes a plain text response.
func (c *Context) String(code int, s string) {
	c.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.W.WriteHeader(code)
	_, _ = c.W.Write([]byte(s))
}

// Status writes only a status code.
func (c *Context) Status(code int) {
	c.W.WriteHeader(code)
}

// FullPath returns the matched route pattern (best effort from URL).
func (c *Context) FullPath() string {
	return c.R.URL.Path
}
