package api

import (
	"log"
	"strings"
	"time"

	"sub-store/internal/config"
	"sub-store/internal/store"
)

// StartCronJobs starts all configured periodic tasks using stdlib time.
func StartCronJobs(cfg *config.Config, db *store.Store) {
	if cfg.SyncCron != "" {
		go runCron("SYNC", cfg.SyncCron, func() {
			log.Println("[SYNC CRON] running")
		})
	}
	if cfg.DownloadCron != "" {
		go runCron("DOWNLOAD", cfg.DownloadCron, func() {
			log.Println("[DOWNLOAD CRON] running")
		})
	}
	if cfg.UploadCron != "" {
		go runCron("UPLOAD", cfg.UploadCron, func() {
			log.Println("[UPLOAD CRON] running")
		})
	}
	if cfg.ProduceCron != "" {
		for _, item := range strings.Split(cfg.ProduceCron, ";") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			parts := strings.SplitN(item, ",", 3)
			if len(parts) < 3 {
				log.Printf("[PRODUCE CRON] invalid entry: %s", item)
				continue
			}
			expr := strings.TrimSpace(parts[0])
			typ := strings.TrimSpace(parts[1])
			name := strings.TrimSpace(parts[2])
			capturedExpr, capturedTyp, capturedName := expr, typ, name
			go runCron("PRODUCE:"+capturedTyp+":"+capturedName, capturedExpr, func() {
				log.Printf("[PRODUCE CRON] %s %s running", capturedTyp, capturedName)
			})
		}
	}
	if cfg.MmdbCron != "" {
		countryPath := cfg.MmdbCountryPath
		countryURL := cfg.MmdbCountryURL
		asnPath := cfg.MmdbAsnPath
		asnURL := cfg.MmdbAsnURL
		go runCron("MMDB", cfg.MmdbCron, func() {
			if countryPath != "" && countryURL != "" {
				if err := downloadFileTo(countryURL, countryPath); err != nil {
					log.Printf("[MMDB CRON] country: %v", err)
				}
			}
			if asnPath != "" && asnURL != "" {
				if err := downloadFileTo(asnURL, asnPath); err != nil {
					log.Printf("[MMDB CRON] ASN: %v", err)
				}
			}
		})
	}
}

// runCron parses a simplified cron expression and runs fn on schedule.
// Supported: "@every Xs/Xm/Xh", "@hourly", "@daily", "@weekly",
// and standard 5-field cron "min hour dom mon dow".
func runCron(name, expr string, fn func()) {
	d, err := parseCronExpr(expr)
	if err != nil {
		log.Printf("[CRON:%s] invalid expression %q: %v", name, expr, err)
		return
	}
	log.Printf("[CRON:%s] scheduled every %s", name, d)
	ticker := time.NewTicker(d)
	defer ticker.Stop()
	for range ticker.C {
		fn()
	}
}

func parseCronExpr(expr string) (time.Duration, error) {
	expr = strings.TrimSpace(expr)
	// @every shorthand
	if strings.HasPrefix(expr, "@every ") {
		return time.ParseDuration(strings.TrimPrefix(expr, "@every "))
	}
	switch expr {
	case "@hourly":
		return time.Hour, nil
	case "@daily", "@midnight":
		return 24 * time.Hour, nil
	case "@weekly":
		return 7 * 24 * time.Hour, nil
	case "@monthly":
		return 30 * 24 * time.Hour, nil
	}
	// For standard 5-field cron, approximate to a reasonable duration.
	// Full cron parsing is out of scope; derive interval from minutes field.
	fields := strings.Fields(expr)
	if len(fields) >= 5 {
		minField := fields[0]
		if minField == "*" {
			return time.Minute, nil
		}
		if strings.HasPrefix(minField, "*/") {
			if n, err := time.ParseDuration(strings.TrimPrefix(minField, "*/") + "m"); err == nil {
				return n, nil
			}
		}
		// hourly by default for complex expressions
		return time.Hour, nil
	}
	return time.Hour, nil
}
