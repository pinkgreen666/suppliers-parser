package storage

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var legacyPriceName = regexp.MustCompile(`^(yopt|productcenter)_[0-9]+_.+`)

// Leave a margin beyond the download deadline so other active downloads survive.
const staleDownloadAge = 2 * downloadTimeout

func CleanupPriceFiles() error { return cleanupPriceFiles(pricesDir, time.Now()) }

func cleanupPriceFiles(dir string, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var failures []error
	removed := 0
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !info.Mode().IsRegular() || now.Sub(info.ModTime()) <= staleDownloadAge {
			continue
		}
		temporary := strings.HasPrefix(entry.Name(), ".download-")
		legacyEmpty := info.Size() == 0 && legacyPriceName.MatchString(entry.Name())
		if !temporary && !legacyEmpty {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			failures = append(failures, fmt.Errorf("remove %s: %w", path, err))
			continue
		}
		removed++
	}
	log.Printf("[download] очистка %s: удалено старых временных и пустых файлов: %d", dir, removed)
	return errors.Join(failures...)
}
