// Sub-Store Go Backend
// Advanced Subscription Manager for QX, Loon, Surge, Clash, Sing-box, etc.
// Go rewrite of https://github.com/sub-store-org/Sub-Store
package main

import (
	"fmt"
	"log"
	"os"

	"sub-store/internal/api"
	"sub-store/internal/config"
	"sub-store/internal/store"
)

const version = "2.14.0-go"

func main() {
	fmt.Printf(`
┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅
     Sub-Store (Go) -- v%s
┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅┅
`, version)

	cfg := config.Load()

	db, err := store.New(cfg.DataPath)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	db.Migrate()

	srv := api.NewServer(cfg, db, version)
	if err := srv.Run(); err != nil {
		log.Printf("Server error: %v", err)
		os.Exit(1)
	}
}
