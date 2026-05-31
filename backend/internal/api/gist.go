package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GistClient wraps GitHub Gist or GitLab Snippet API.
type GistClient struct {
	Token       string
	Platform    string // "github" | "gitlab"
	GithubProxy string
	APIBaseURL  string
}

func NewGistClient(token, platform, githubProxy, apiBaseURL string) *GistClient {
	return &GistClient{
		Token:       token,
		Platform:    platform,
		GithubProxy: githubProxy,
		APIBaseURL:  apiBaseURL,
	}
}

func (g *GistClient) baseURL() string {
	if g.APIBaseURL != "" {
		return g.APIBaseURL
	}
	if g.Platform == "gitlab" {
		return "https://gitlab.com/api/v4"
	}
	if g.GithubProxy != "" {
		return g.GithubProxy + "/api.github.com"
	}
	return "https://api.github.com"
}

func (g *GistClient) authHeader() string {
	if g.Platform == "gitlab" {
		return "Bearer " + g.Token
	}
	return "token " + g.Token
}

type gistFile struct {
	Content string `json:"content"`
}

type gistPayload struct {
	Description string              `json:"description"`
	Public      bool                `json:"public"`
	Files       map[string]gistFile `json:"files"`
}

// FindGist looks for a Gist/Snippet with the given description key.
func (g *GistClient) FindGist(description string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	var reqURL string
	if g.Platform == "gitlab" {
		reqURL = g.baseURL() + "/snippets?per_page=100"
	} else {
		reqURL = g.baseURL() + "/gists?per_page=100"
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", g.authHeader())
	req.Header.Set("User-Agent", "Sub-Store/go")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var items []map[string]interface{}
	if err := json.Unmarshal(body, &items); err != nil {
		return "", fmt.Errorf("parse gist list: %w", err)
	}

	for _, item := range items {
		desc, _ := item["description"].(string)
		if desc == description {
			id, _ := item["id"].(string)
			return id, nil
		}
	}
	return "", nil
}

// UploadGist creates or updates a Gist with the given file content.
func (g *GistClient) UploadGist(description, filename, content string) error {
	client := &http.Client{Timeout: 30 * time.Second}

	existingID, err := g.FindGist(description)
	if err != nil {
		return err
	}

	payload := gistPayload{
		Description: description,
		Public:      false,
		Files: map[string]gistFile{
			filename: {Content: content},
		},
	}
	raw, _ := json.Marshal(payload)

	var method, reqURL string
	if existingID != "" {
		method = "PATCH"
		if g.Platform == "gitlab" {
			reqURL = g.baseURL() + "/snippets/" + existingID
		} else {
			reqURL = g.baseURL() + "/gists/" + existingID
		}
	} else {
		method = "POST"
		if g.Platform == "gitlab" {
			reqURL = g.baseURL() + "/snippets"
		} else {
			reqURL = g.baseURL() + "/gists"
		}
	}

	req, err := http.NewRequest(method, reqURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", g.authHeader())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Sub-Store/go")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("gist API error %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// DownloadGist fetches the content of a Gist file.
func (g *GistClient) DownloadGist(description, filename string) (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	gistID, err := g.FindGist(description)
	if err != nil {
		return "", err
	}
	if gistID == "" {
		return "", fmt.Errorf("gist not found: %s", description)
	}

	var reqURL string
	if g.Platform == "gitlab" {
		reqURL = g.baseURL() + "/snippets/" + gistID + "/raw"
	} else {
		reqURL = g.baseURL() + "/gists/" + gistID
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", g.authHeader())
	req.Header.Set("User-Agent", "Sub-Store/go")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if g.Platform == "gitlab" {
		return string(body), nil
	}

	var gistData map[string]interface{}
	if err := json.Unmarshal(body, &gistData); err != nil {
		return "", err
	}
	files, _ := gistData["files"].(map[string]interface{})
	if files == nil {
		return "", fmt.Errorf("no files in gist")
	}
	for _, f := range files {
		fm, _ := f.(map[string]interface{})
		if content, ok := fm["content"].(string); ok {
			return content, nil
		}
	}
	return "", fmt.Errorf("file %s not found in gist", filename)
}
