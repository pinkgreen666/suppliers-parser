package storage

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const pricesDir = "prices"
const downloadTimeout = 5 * time.Minute

func DownloadPriceFile(ctx context.Context, client *http.Client, rawURL, prefix string) (string, error) {
	return downloadPriceFile(ctx, client, rawURL, prefix, pricesDir)
}

func downloadPriceFile(ctx context.Context, client *http.Client, rawURL, prefix, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	base := sanitizeFileName(filepath.Base(u.Path))
	if base == "" || base == "." || base == "_" {
		base = "price"
	}
	// Bound filename length and distinguish identical names from different URLs.
	ext := filepath.Ext(base)
	if len(ext) > 16 {
		ext = ""
	}
	if len(base) > 140 {
		base = string([]rune(base)[:min(40, len([]rune(base)))]) + ext
	}
	sum := sha256.Sum256([]byte(rawURL))
	localPath := filepath.Join(dir, fmt.Sprintf("%s%x_%s", prefix, sum[:8], base))
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	// Disable Go's transparent decompression: some servers label raw PDFs as gzip.
	req.Header.Set("Accept-Encoding", "identity")
	downloadClient := *client
	downloadClient.Timeout = downloadTimeout
	started := time.Now()
	log.Printf("[download] начало %s (таймаут %s)", rawURL, downloadTimeout)
	var bytes atomic.Int64
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				log.Printf("[download] %s: %d байт, прошло %s", rawURL, bytes.Load(), time.Since(started).Round(time.Second))
			}
		}
	}()
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("status %d for %s", resp.StatusCode, rawURL)
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return "", fmt.Errorf("expected file, got html (%s)", rawURL)
	}
	reader := bufio.NewReader(resp.Body)
	var body io.Reader = reader
	if strings.EqualFold(strings.TrimSpace(resp.Header.Get("Content-Encoding")), "gzip") {
		magic, _ := reader.Peek(2)
		if len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
			gz, err := gzip.NewReader(reader)
			if err != nil {
				return "", fmt.Errorf("gzip: %w", err)
			}
			defer gz.Close()
			body = gz
		} else {
			log.Printf("[download] %s: сервер указал gzip без gzip-сигнатуры; читаем исходный файл", rawURL)
		}
	}
	// Reject HTML error pages even when the server labels them as binary files.
	decoded := bufio.NewReader(body)
	head, _ := decoded.Peek(512)
	if strings.Contains(http.DetectContentType(head), "text/html") {
		return "", fmt.Errorf("expected file, got HTML body (%s)", rawURL)
	}
	f, err := os.CreateTemp(dir, ".download-*")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	defer func() {
		if err := os.Remove(f.Name()); err != nil && !os.IsNotExist(err) {
			log.Printf("[download] не удалось удалить временный файл %s: %v", f.Name(), err)
		}
	}()
	defer f.Close()
	n, err := io.Copy(f, &countingReader{reader: decoded, count: &bytes})
	if err != nil {
		return "", fmt.Errorf("copy: %w", err)
	}
	if n == 0 {
		return "", fmt.Errorf("empty file: %s", rawURL)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close: %w", err)
	}
	if err := os.Rename(f.Name(), localPath); err != nil {
		return "", fmt.Errorf("rename: %w", err)
	}
	log.Printf("[download] готово %s: %d байт за %s → %s", rawURL, n, time.Since(started).Round(time.Second), localPath)
	return localPath, nil
}

type countingReader struct {
	reader io.Reader
	count  *atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.count.Add(int64(n))
	return n, err
}

func sanitizeFileName(name string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_", " ", "_")
	return repl.Replace(name)
}
