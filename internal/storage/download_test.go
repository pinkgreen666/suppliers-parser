package storage

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadEncoding(t *testing.T) {
	content := []byte("%PDF-1.4\nexample document")
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	gz.Write(content)
	gz.Close()
	for _, tc := range []struct {
		name, encoding string
		body           []byte
	}{
		{"plain", "", content}, {"real gzip", "gzip", compressed.Bytes()}, {"mislabeled gzip", "gzip", content},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Timeout: time.Nanosecond, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if got := r.Header.Get("Accept-Encoding"); got != "identity" {
					t.Errorf("Accept-Encoding=%q", got)
				}
				deadline, ok := r.Context().Deadline()
				if !ok || time.Until(deadline) < time.Minute {
					t.Error("download inherited short page timeout")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": {tc.encoding}}, Body: io.NopCloser(bytes.NewReader(tc.body))}, nil
			})}
			dir := t.TempDir()
			path, err := downloadPriceFile(context.Background(), client, "https://example.test/uploads/%D0%9F%D1%80%D0%B0%D0%B9%D1%81.pdf", "test_", dir)
			if err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("got %q, err=%v", got, err)
			}
			if !strings.HasSuffix(path, "Прайс.pdf") {
				t.Errorf("filename not decoded: %s", path)
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 1 {
				t.Fatalf("unexpected temporary files: %v", files)
			}
			if client.Timeout != time.Nanosecond {
				t.Error("shared client was mutated")
			}
		})
	}
}

type brokenBody struct{ io.Reader }

func (b brokenBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return n, err
}
func (b brokenBody) Close() error { return nil }

func TestDownloadFailureLeavesNoPartialFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		body func() io.ReadCloser
	}{
		{"truncated", func() io.ReadCloser { return brokenBody{strings.NewReader("%PDF-partial")} }},
		{"empty", func() io.ReadCloser { return io.NopCloser(strings.NewReader("")) }},
		{"HTML", func() io.ReadCloser { return io.NopCloser(strings.NewReader("<!DOCTYPE html><html>blocked</html>")) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: tc.body()}, nil
			})}
			if _, err := downloadPriceFile(context.Background(), client, "https://example.test/file.pdf", "", dir); err == nil {
				t.Fatal("expected failure")
			}
			files, _ := filepath.Glob(filepath.Join(dir, "*"))
			if len(files) != 0 {
				t.Fatalf("left files: %v", files)
			}
		})
	}
}

func TestDownloadCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	defer cancel()
	_, err := downloadPriceFile(ctx, client, "https://example.test/file.pdf", "", t.TempDir())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

type cancelDuringCopyBody struct {
	ctx    context.Context
	cancel context.CancelFunc
	dir    string
	t      *testing.T
	sent   bool
}

func (b *cancelDuringCopyBody) Read(p []byte) (int, error) {
	if !b.sent {
		b.sent = true
		return copy(p, "%PDF-1.4\n"+strings.Repeat("x", 4096)), nil
	}
	files, err := filepath.Glob(filepath.Join(b.dir, ".download-*"))
	if err != nil || len(files) != 1 {
		b.t.Errorf("expected active temporary file before cancellation: %v, %v", files, err)
	}
	b.cancel()
	return 0, b.ctx.Err()
}
func (b *cancelDuringCopyBody) Close() error { return nil }

func TestCancellationDuringCopyRemovesTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: &cancelDuringCopyBody{ctx: ctx, cancel: cancel, dir: dir, t: t}}, nil
	})}
	_, err := downloadPriceFile(ctx, client, "https://example.test/file.pdf", "", dir)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected cancellation, got %v", err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 0 {
		t.Fatalf("files left after cancellation: %v, %v", files, err)
	}
}
