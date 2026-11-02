package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCleanupOnlyStaleDownloadArtifacts(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for _, tc := range []struct {
		name, content string
		old, removed  bool
	}{
		{".download-abandoned", "partial", true, true},
		{".download-active", "partial", false, false},
		{"productcenter_519_old.pdf", "", true, true},
		{"yopt_12_old.pdf", "", true, true},
		{"productcenter_519_active.pdf", "", false, false},
		{"productcenter_519_good.pdf", "%PDF", true, false},
		{"personal.txt", "", true, false},
	} {
		path := filepath.Join(dir, tc.name)
		if err := os.WriteFile(path, []byte(tc.content), 0600); err != nil {
			t.Fatal(err)
		}
		if tc.old {
			old := now.Add(-staleDownloadAge - time.Minute)
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
		if err := cleanupPriceFiles(dir, now); err != nil {
			t.Fatal(err)
		}
		_, err := os.Stat(path)
		if tc.removed != os.IsNotExist(err) {
			t.Errorf("%s removed=%v err=%v", tc.name, tc.removed, err)
		}
	}
}
