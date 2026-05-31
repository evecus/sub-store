package proxy

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"sub-store/internal/store"
)

// firstLine returns the first non-empty line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return strings.TrimSpace(s)
}

// FetchContent downloads a URL and returns the raw body + response headers.
func FetchContent(rawURL string, timeout int, proxySetting string) (string, map[string]string, error) {
	insecure := strings.Contains(rawURL, "#insecure")
	cleanURL := strings.Split(strings.TrimSuffix(rawURL, "#insecure"), "#")[0]

	if timeout <= 0 {
		timeout = 15000
	}
	duration := time.Duration(timeout) * time.Millisecond

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}
	client := &http.Client{Timeout: duration, Transport: tr}

	req, err := http.NewRequest("GET", cleanURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "ClashMeta/alpha")
	req.Header.Set("Accept", "*/*")

	log.Printf("[fetch] GET %s", cleanURL)
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("fetching %s: %w", cleanURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, cleanURL)
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

// FetchAndParse fetches a subscription, parses it, and applies all operators.
func FetchAndParse(sub map[string]interface{}, db *store.Store) ([]Proxy, error) {
	source, _ := sub["source"].(string)

	var content string
	var respHeaders map[string]string

	if source == "local" {
		content, _ = sub["content"].(string)
	} else {
		rawURL, _ := sub["url"].(string)
		rawURL = firstLine(rawURL)

		settings := db.ReadMap(store.KeySettings)
		timeout, _ := settings["defaultTimeout"].(float64)
		proxySetting, _ := sub["proxy"].(string)

		var err error
		content, respHeaders, err = FetchWithCache(db, rawURL, int(timeout), proxySetting)
		if err != nil {
			return nil, err
		}

		// Store flow info back into sub for header passthrough
		if ui := respHeaders["subscription-userinfo"]; ui != "" {
			sub["_flowInfo"] = map[string]interface{}{"subscription-userinfo": ui}
		}
	}

	proxies, err := ParseContent(content, "")
	if err != nil {
		return nil, err
	}

	// Apply operators defined in the subscription
	if ops, ok := sub["process"].([]interface{}); ok && len(ops) > 0 {
		var logs []string
		proxies, logs = ProcessProxies(proxies, ops)
		for _, l := range logs {
			log.Printf("[process] %s", l)
		}
	}

	// Apply global operators from settings if defined
	settings := db.ReadMap(store.KeySettings)
	if globalOps, ok := settings["globalProcessors"].([]interface{}); ok && len(globalOps) > 0 {
		var logs []string
		proxies, logs = ProcessProxies(proxies, globalOps)
		for _, l := range logs {
			log.Printf("[global-process] %s", l)
		}
	}

	return proxies, nil
}

// FetchAndParseCollection fetches all subscriptions in a collection and merges them.
func FetchAndParseCollection(col map[string]interface{}, db *store.Store) ([]Proxy, error) {
	subsRaw, _ := col["subscriptions"].([]interface{})
	allSubs := db.ReadMapSlice(store.KeySubs)

	var result []Proxy
	for _, nameRaw := range subsRaw {
		subName, _ := nameRaw.(string)
		if subName == "" {
			continue
		}
		sub, _ := store.FindByName(allSubs, subName)
		if sub == nil {
			log.Printf("[collection] subscription not found: %s", subName)
			continue
		}
		proxies, err := FetchAndParse(sub, db)
		if err != nil {
			log.Printf("[collection] failed to fetch subscription %s: %v", subName, err)
			continue
		}
		result = append(result, proxies...)
	}

	// Apply collection-level operators
	if ops, ok := col["process"].([]interface{}); ok && len(ops) > 0 {
		var logs []string
		result, logs = ProcessProxies(result, ops)
		for _, l := range logs {
			log.Printf("[col-process] %s", l)
		}
	}

	return result, nil
}
