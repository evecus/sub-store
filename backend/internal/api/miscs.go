package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"sub-store/internal/store"
)

type MiscHandler struct {
	db      *store.Store
	version string
}

func (h *MiscHandler) getEnv(c *Context) {
	env := map[string]interface{}{
		"version": h.version,
		"isNode":  true,
		"feature": map[string]interface{}{
			"archive": true,
			"share":   c.Query("share") == "true",
		},
		"guide": "⚠️ Sub-Store Go Backend. Frontend: https://sub-store.vercel.app",
	}
	c.JSON(http.StatusOK, map[string]interface{}{"status": "success", "data": env})
}

func (h *MiscHandler) refresh(c *Context) {
	_ = h.db.Delete(store.KeyResourceCache)
	_ = h.db.Delete(store.KeyHeadersResourceCache)
	_ = h.db.Delete(store.KeyScriptResourceCache)
	log.Println("[refresh] all caches cleared")
	successNoData(c)
}

func (h *MiscHandler) gistBackup(c *Context) {
	action := c.DefaultQuery("action", "upload")
	settings := h.db.ReadMap(store.KeySettings)
	token, _ := settings["gistToken"].(string)
	if token == "" {
		failed(c, APIError{Code: "NO_GIST_TOKEN", Type: "RequestInvalidError",
			Message: "GitHub token is required for Gist backup"}, http.StatusBadRequest)
		return
	}
	platform, _ := settings["syncPlatform"].(string)
	if platform == "" {
		platform = "github"
	}
	githubProxy, _ := settings["githubProxy"].(string)
	apiURL, _ := settings["githubApiUrl"].(string)
	gist := NewGistClient(token, platform, githubProxy, apiURL)

	switch action {
	case "upload":
		allData := h.db.RawData()
		raw, _ := json.MarshalIndent(allData, "", "  ")
		encoded := base64.StdEncoding.EncodeToString(raw)
		if err := gist.UploadGist(store.GistBackupKey, store.GistBackupFileName, encoded); err != nil {
			log.Printf("[gist] upload failed: %v", err)
			failed(c, errInternal("Gist upload failed", err.Error()))
			return
		}
		log.Printf("[gist] uploaded %d bytes", len(raw))
		success(c, map[string]string{"action": "upload", "message": "Gist upload completed"})
	case "download":
		content, err := gist.DownloadGist(store.GistBackupKey, store.GistBackupFileName)
		if err != nil {
			log.Printf("[gist] download failed: %v", err)
			failed(c, errInternal("Gist download failed", err.Error()))
			return
		}
		if err := h.restoreFromString(content); err != nil {
			failed(c, errInternal("Restore failed", err.Error()))
			return
		}
		log.Printf("[gist] data restored from Gist")
		success(c, map[string]string{"action": "download", "message": "Gist restore completed"})
	default:
		failed(c, APIError{Code: "INVALID_ACTION", Type: "RequestInvalidError",
			Message: "Unknown action: " + action}, http.StatusBadRequest)
	}
}

func (h *MiscHandler) exportStorage(c *Context) {
	allData := h.db.RawData()
	raw, err := json.MarshalIndent(allData, "", "  ")
	if err != nil {
		failed(c, errInternal("Failed to serialize storage", err.Error()))
		return
	}
	fname := "sub-store_data_" + formatDateTime_now() + ".json"
	c.SetHeader("Content-Disposition", `attachment; filename="`+fname+`"`)
	c.Data(http.StatusOK, "application/json", raw)
}

func (h *MiscHandler) importStorage(c *Context) {
	var body struct {
		Content interface{} `json:"content"`
	}
	if err := c.BindJSON(&body); err != nil {
		failed(c, APIError{Code: "INVALID_BACKUP_DATA", Type: "RequestInvalidError",
			Message: "Invalid backup data format", Details: err.Error()}, http.StatusBadRequest)
		return
	}
	var dataMap map[string]json.RawMessage
	switch v := body.Content.(type) {
	case string:
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			decoded = []byte(v)
		}
		if err := json.Unmarshal(decoded, &dataMap); err != nil {
			failed(c, APIError{Code: "INVALID_BACKUP_DATA", Type: "RequestInvalidError",
				Message: "备份文件校验失败, 无法还原", Details: err.Error()}, http.StatusBadRequest)
			return
		}
	case map[string]interface{}:
		raw, _ := json.Marshal(v)
		if err := json.Unmarshal(raw, &dataMap); err != nil {
			failed(c, APIError{Code: "INVALID_BACKUP_DATA", Type: "RequestInvalidError",
				Message: "备份文件校验失败, 无法还原", Details: err.Error()}, http.StatusBadRequest)
			return
		}
	default:
		failed(c, APIError{Code: "INVALID_BACKUP_DATA", Type: "RequestInvalidError",
			Message: "备份文件校验失败 - 未知格式"}, http.StatusBadRequest)
		return
	}
	if _, ok := dataMap[store.KeySettings]; !ok {
		failed(c, APIError{Code: "INVALID_BACKUP_DATA", Type: "RequestInvalidError",
			Message: "备份文件应该至少包含 settings 字段"}, http.StatusBadRequest)
		return
	}
	if err := h.db.RestoreRaw(dataMap); err != nil {
		failed(c, errInternal("Failed to restore backup", err.Error()))
		return
	}
	h.db.Migrate()
	log.Println("[storage] data restored from backup")
	successNoData(c)
}

func (h *MiscHandler) restoreFromString(content string) error {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(content))
	if err != nil {
		decoded = []byte(content)
	}
	var dataMap map[string]json.RawMessage
	if err := json.Unmarshal(decoded, &dataMap); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}
	if _, ok := dataMap[store.KeySettings]; !ok {
		return fmt.Errorf("备份文件应该至少包含 settings 字段")
	}
	if err := h.db.RestoreRaw(dataMap); err != nil {
		return err
	}
	h.db.Migrate()
	return nil
}
