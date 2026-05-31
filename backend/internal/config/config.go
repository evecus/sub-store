package config

import (
	"os"
	"path/filepath"
	"strconv"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	// Backend HTTP listen host and port
	Host string
	Port int

	// Path to the JSON data file
	DataPath string

	// Optional URL to download initial data from on startup
	DataURL string

	// Optional path to serve static frontend files
	FrontendPath string
	// Port for separate frontend server (default 3001)
	FrontendPort int
	FrontendHost string
	// Path prefix that routes frontend backend API calls (e.g. /sub-store)
	FrontendBackendPath string

	// Merge frontend and backend on the same port
	BackendMerge bool

	// Cron expressions
	SyncCron     string
	DownloadCron string
	UploadCron   string
	ProduceCron  string
	MmdbCron     string

	// MaxDB file paths & URLs
	MmdbCountryPath string
	MmdbCountryURL  string
	MmdbAsnPath     string
	MmdbAsnURL      string
}

func Load() *Config {
	cfg := &Config{
		Host:     getEnv("SUB_STORE_BACKEND_API_HOST", "0.0.0.0"),
		Port:     getEnvInt("SUB_STORE_BACKEND_API_PORT", 3000),
		DataPath: getEnv("SUB_STORE_DATA_PATH", defaultDataPath()),

		DataURL: getEnv("SUB_STORE_DATA_URL", ""),

		FrontendPath:        getEnv("SUB_STORE_FRONTEND_PATH", ""),
		FrontendPort:        getEnvInt("SUB_STORE_FRONTEND_PORT", 3001),
		FrontendHost:        getEnv("SUB_STORE_FRONTEND_HOST", "0.0.0.0"),
		FrontendBackendPath: getEnv("SUB_STORE_FRONTEND_BACKEND_PATH", ""),
		BackendMerge:        getEnvBool("SUB_STORE_BACKEND_MERGE", false),

		SyncCron:     getEnv("SUB_STORE_BACKEND_SYNC_CRON", ""),
		DownloadCron: getEnv("SUB_STORE_BACKEND_DOWNLOAD_CRON", ""),
		UploadCron:   getEnv("SUB_STORE_BACKEND_UPLOAD_CRON", ""),
		ProduceCron:  getEnv("SUB_STORE_PRODUCE_CRON", ""),
		MmdbCron:     getEnv("SUB_STORE_MMDB_CRON", ""),

		MmdbCountryPath: getEnv("SUB_STORE_MMDB_COUNTRY_PATH", ""),
		MmdbCountryURL:  getEnv("SUB_STORE_MMDB_COUNTRY_URL", ""),
		MmdbAsnPath:     getEnv("SUB_STORE_MMDB_ASN_PATH", ""),
		MmdbAsnURL:      getEnv("SUB_STORE_MMDB_ASN_URL", ""),
	}
	return cfg
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "1" || v == "true" || v == "yes"
	}
	return fallback
}

func defaultDataPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "./sub-store-data.json"
	}
	return filepath.Join(home, ".sub-store", "data.json")
}
