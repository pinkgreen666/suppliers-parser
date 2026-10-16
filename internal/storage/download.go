package storage

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const pricesDir = "prices"

func DownloadPriceFile(client *http.Client, rawURL, prefix string) (string, error) {
	if err := os.MkdirAll(pricesDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", pricesDir, err)
	}

	base := filepath.Base(rawURL)
	if i := strings.IndexAny(base, "?#"); i >= 0 {
		base = base[:i]
	}
	base = sanitizeFileName(base)
	if base == "" || base == "." {
		base = fmt.Sprintf("price_%d.xlsx", time.Now().Unix())
	}

	name := prefix + base
	localPath := filepath.Join(pricesDir, name)

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d for %s", resp.StatusCode, rawURL)
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/html") {
		return "", fmt.Errorf("expected file, got html (%s)", rawURL)
	}

	f, err := os.Create(localPath)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", localPath, err)
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return "", fmt.Errorf("copy: %w", err)
	}
	if n == 0 {
		os.Remove(localPath)
		return "", fmt.Errorf("empty file: %s", rawURL)
	}

	return localPath, nil
}

func sanitizeFileName(name string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	return repl.Replace(name)
}
