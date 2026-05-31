package api

import (
	"math"
	"net/http"

	"sub-store/internal/store"
)

type SettingsHandler struct{ db *store.Store }

func (h *SettingsHandler) get(c *Context) {
	success(c, h.db.ReadMap(store.KeySettings))
}

func (h *SettingsHandler) update(c *Context) {
	var body map[string]interface{}
	if err := c.BindJSON(&body); err != nil {
		failed(c, errInternal("Invalid request body", err.Error()), http.StatusBadRequest)
		return
	}
	settings := h.db.ReadMap(store.KeySettings)
	for k, v := range body {
		settings[k] = v
	}
	numericKeys := []string{
		"defaultTimeout", "githubApiTimeout", "artifactSyncBatchSize",
		"cacheThreshold", "resourceCacheTtl", "headersCacheTtl", "scriptCacheTtl",
	}
	for _, key := range numericKeys {
		if v, ok := settings[key]; ok {
			n := toFloat64(v)
			if math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 {
				delete(settings, key)
			} else {
				settings[key] = n
			}
		}
	}
	if v, ok := settings["logsMaxCount"]; ok {
		if v == nil {
			delete(settings, "logsMaxCount")
		} else {
			n := toFloat64(v)
			if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
				delete(settings, "logsMaxCount")
			} else {
				settings["logsMaxCount"] = n
			}
		}
	}
	if err := h.db.Write(store.KeySettings, settings); err != nil {
		failed(c, errInternal("Failed to update settings", err.Error()))
		return
	}
	success(c, settings)
}

func toFloat64(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	return math.NaN()
}
