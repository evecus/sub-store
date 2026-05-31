package api

import (
	"io"
	"net/http"
	"os"
	"time"
)

func writeFile(path string, data []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

func downloadFileTo(srcURL, destPath string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(srcURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return writeFile(destPath, data)
}
