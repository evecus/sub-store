// Package api wires up the HTTP server and registers all REST routes.
package api

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"sub-store/internal/config"
	"sub-store/internal/static"
	"sub-store/internal/store"
)

// Server is the top-level HTTP server.
type Server struct {
	cfg     *config.Config
	db      *store.Store
	version string
	mux     *Router
}

// NewServer creates a Server and registers all routes.
func NewServer(cfg *config.Config, db *store.Store, version string) *Server {
	r := NewRouter()
	s := &Server{cfg: cfg, db: db, version: version, mux: r}
	s.registerRoutes()
	if cfg.BackendMerge {
		s.registerStaticFrontend()
		log.Printf("[FRONTEND] embedded frontend enabled on same port")
	}
	return s
}

// Run starts the HTTP server (blocking).
func (s *Server) Run() error {
	StartCronJobs(s.cfg, s.db)
	if s.cfg.DataURL != "" {
		go s.restoreFromURL(s.cfg.DataURL)
	}
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	log.Printf("[BACKEND] listening on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}

func (s *Server) restoreFromURL(dataURL string) {
	log.Printf("[BACKEND] downloading data from %s", dataURL)
	content, _, err := fetchURL(dataURL)
	if err != nil {
		log.Printf("[BACKEND] restore data failed: %v", err)
		return
	}
	m := &MiscHandler{db: s.db, version: s.version}
	if err := m.restoreFromString(content); err != nil {
		log.Printf("[BACKEND] restore data failed: %v", err)
		return
	}
	log.Printf("[BACKEND] restored data from %s", dataURL)
}

// registerStaticFrontend mounts the embedded frontend under "/"
// and falls back to index.html for unknown paths (SPA support).
func (s *Server) registerStaticFrontend() {
	distFS, err := fs.Sub(static.FS, "dist")
	if err != nil {
		log.Printf("[FRONTEND] warning: embedded frontend dist not found: %v", err)
		return
	}
	fileServer := http.FileServer(http.FS(distFS))

	s.mux.SetFallback(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		// API/download/share paths stay 404 from the router
		if strings.HasPrefix(p, "/api/") ||
			strings.HasPrefix(p, "/download/") ||
			strings.HasPrefix(p, "/share/") {
			http.NotFound(w, r)
			return
		}

		// Try the real static file first
		cleanPath := strings.TrimPrefix(p, "/")
		if cleanPath == "" {
			cleanPath = "index.html"
		}
		f, openErr := distFS.Open(cleanPath)
		if openErr == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html so Vue Router handles the route
		r2 := *r
		r2.URL = *r.URL
		r2.URL.Path = "/"
		fileServer.ServeHTTP(w, &r2)
	})
}

func (s *Server) registerRoutes() {
	r := s.mux

	// Misc
	m := &MiscHandler{db: s.db, version: s.version}
	r.GET("/", m.getEnv)
	r.GET("/api/utils/env", m.getEnv)
	r.GET("/api/utils/refresh", m.refresh)
	r.GET("/api/utils/backup", m.gistBackup)
	r.GET("/api/storage", m.exportStorage)
	r.POST("/api/storage", m.importStorage)

	// Subscriptions
	sub := &SubHandler{db: s.db}
	r.GET("/api/sub/flow/:name", sub.getFlowInfo)
	r.GET("/api/sub/:name", sub.getOne)
	r.PATCH("/api/sub/:name", sub.update)
	r.DELETE("/api/sub/:name", sub.delete)
	r.GET("/api/subs", sub.getAll)
	r.POST("/api/subs", sub.create)
	r.PUT("/api/subs", sub.replace)

	// Collections
	col := &ColHandler{db: s.db}
	r.GET("/api/collection/:name", col.getOne)
	r.PATCH("/api/collection/:name", col.update)
	r.DELETE("/api/collection/:name", col.delete)
	r.GET("/api/collections", col.getAll)
	r.POST("/api/collections", col.create)
	r.PUT("/api/collections", col.replace)

	// Settings
	set := &SettingsHandler{db: s.db}
	r.GET("/api/settings", set.get)
	r.PATCH("/api/settings", set.update)

	// Artifacts
	art := &ArtifactHandler{db: s.db}
	r.GET("/api/artifact/:name", art.getOne)
	r.PATCH("/api/artifact/:name", art.update)
	r.DELETE("/api/artifact/:name", art.delete)
	r.GET("/api/artifacts", art.getAll)
	r.POST("/api/artifacts", art.create)
	r.PUT("/api/artifacts", art.replace)
	r.GET("/api/sync/artifacts", art.syncAll)
	r.GET("/api/sync/artifact/:name", art.syncOne)

	// Files
	file := &FileHandler{db: s.db}
	r.GET("/api/file/:name", file.getOne)
	r.PATCH("/api/file/:name", file.update)
	r.DELETE("/api/file/:name", file.delete)
	r.GET("/api/files", file.getAll)
	r.POST("/api/files", file.create)
	r.PUT("/api/files", file.replace)

	// Modules
	mod := &ModuleHandler{db: s.db}
	r.GET("/api/module/:name", mod.getOne)
	r.PATCH("/api/module/:name", mod.update)
	r.DELETE("/api/module/:name", mod.delete)
	r.GET("/api/modules", mod.getAll)
	r.POST("/api/modules", mod.create)
	r.PUT("/api/modules", mod.replace)

	// Tokens
	tok := &TokenHandler{db: s.db}
	r.GET("/api/token/:name", tok.getOne)
	r.PATCH("/api/token/:name", tok.update)
	r.DELETE("/api/token/:name", tok.delete)
	r.GET("/api/tokens", tok.getAll)
	r.POST("/api/tokens", tok.create)
	r.PUT("/api/tokens", tok.replace)
	r.POST("/api/token/generate", tok.generate)

	// Archives
	arch := &ArchiveHandler{db: s.db}
	r.GET("/api/archives", arch.getAll)
	r.DELETE("/api/archive/:name", arch.delete)
	r.POST("/api/archive/:name/restore", arch.restore)

	// Logs
	logH := &LogHandler{db: s.db}
	r.GET("/api/logs", logH.get)
	r.DELETE("/api/logs", logH.clear)

	// Node info
	ni := &NodeInfoHandler{db: s.db}
	r.GET("/api/node_info", ni.get)

	// Sort
	sort := &SortHandler{db: s.db}
	r.POST("/api/subs/sort", sort.sortSubs)
	r.POST("/api/collections/sort", sort.sortCols)

	// Preview
	prev := &PreviewHandler{db: s.db}
	r.GET("/api/preview/sub/:name", prev.previewSub)
	r.GET("/api/preview/collection/:name", prev.previewCol)

	// Parser
	par := &ParserHandler{}
	r.POST("/api/utils/parser", par.parse)

	// Cache invalidation
	cacheH := &CacheHandler{db: s.db}
	r.DELETE("/api/cache/sub/:name", cacheH.invalidateSub)
	r.DELETE("/api/cache/collection/:name", cacheH.invalidateCol)
	r.DELETE("/api/cache", cacheH.invalidateAll)

	// Rules
	ruleH := &RuleHandler{db: s.db}
	r.GET("/api/rules", ruleH.getAll)
	r.POST("/api/rules", ruleH.create)
	r.GET("/api/rule/:name", ruleH.getOne)
	r.PATCH("/api/rule/:name", ruleH.update)
	r.DELETE("/api/rule/:name", ruleH.delete)
	r.PUT("/api/rules", ruleH.replace)

	// Download / Share
	dl := &DownloadHandler{db: s.db, version: s.version}
	r.GET("/download/:name", dl.downloadSub)
	r.GET("/download/:name/:target", dl.downloadSubWithTarget)
	r.GET("/share/sub/:name", dl.downloadSub)
	r.GET("/share/sub/:name/:target", dl.downloadSubWithTarget)
	r.GET("/download/collection/:name", dl.downloadCol)
	r.GET("/download/collection/:name/:target", dl.downloadColWithTarget)
	r.GET("/share/col/:name", dl.downloadCol)
	r.GET("/share/col/:name/:target", dl.downloadColWithTarget)
	r.GET("/download/:name/api/v1/server/details", dl.nezhaServerDetails)
	r.GET("/download/collection/:name/api/v1/server/details", dl.nezhaServerDetailsCol)
}
