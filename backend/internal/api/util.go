package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func mergeMaps(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(base)+len(override))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		result[k] = v
	}
	return result
}

func copyMap(m map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func formatDateTime(t time.Time) string { return t.Format("20060102_150405") }
func formatDateTime_now() string        { return formatDateTime(time.Now()) }
func nowMillis() int64                  { return time.Now().UnixMilli() }

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(s)
}

func parseURLArguments(rawURL string) (string, map[string]interface{}) {
	parts := strings.SplitN(rawURL, "#", 2)
	baseURL := parts[0]
	args := make(map[string]interface{})
	if len(parts) < 2 || parts[1] == "" {
		return baseURL, args
	}
	fragment := parts[1]
	decoded, err := url.QueryUnescape(fragment)
	if err != nil {
		decoded = fragment
	}
	var jsonArgs map[string]interface{}
	if err := json.Unmarshal([]byte(decoded), &jsonArgs); err == nil {
		return baseURL, jsonArgs
	}
	for _, pair := range strings.Split(fragment, "&") {
		idx := strings.IndexByte(pair, '=')
		if idx == -1 {
			args[pair] = true
			continue
		}
		k, v := pair[:idx], pair[idx+1:]
		if v == "" {
			args[k] = true
		} else {
			dv, _ := url.QueryUnescape(v)
			args[k] = dv
		}
	}
	return baseURL, args
}

func parseFlowHeaders(raw string) map[string]interface{} {
	if raw == "" {
		return nil
	}
	result := make(map[string]interface{})
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		idx := strings.IndexByte(part, '=')
		if idx == -1 {
			continue
		}
		k := strings.TrimSpace(part[:idx])
		v := strings.TrimSpace(part[idx+1:])
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			result[k] = n
		} else {
			result[k] = v
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func fetchFlowHeaders(targetURL, _ string) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: strings.Contains(targetURL, "#insecure")},
		},
	}
	cleanURL := strings.TrimSuffix(targetURL, "#insecure")
	for _, method := range []string{"HEAD", "GET"} {
		req, err := http.NewRequest(method, cleanURL, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", "ClashMeta/1.0")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		for _, hdr := range []string{"subscription-userinfo", "Subscription-Userinfo"} {
			if v := resp.Header.Get(hdr); v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("no subscription-userinfo header")
}

func fetchURL(rawURL string) (string, map[string]string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", "Sub-Store/go")
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, rawURL)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 20*1024*1024))
	if err != nil {
		return "", nil, err
	}
	headers := map[string]string{
		"subscription-userinfo": resp.Header.Get("subscription-userinfo"),
		"content-type":          resp.Header.Get("content-type"),
	}
	return string(body), headers, nil
}
