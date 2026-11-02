package storage

import (
	"os"
	"path/filepath"
	"testing"

	"suppliers-parser/internal/models"
)

func TestPendingPriceFilesAndPreserveDownloadedPath(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	migrations, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil || len(migrations) == 0 {
		t.Fatalf("migrations: %v", err)
	}
	for _, path := range migrations {
		sql, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.sql.Exec(string(sql)); err != nil {
			t.Fatal(err)
		}
	}
	id, err := db.SaveCompany(models.Company{Source: "yopt", URL: "https://example.test/company"})
	if err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good.pdf")
	empty := filepath.Join(dir, "empty.pdf")
	if err := os.WriteFile(good, []byte("%PDF-file"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for i, path := range []string{"", filepath.Join(dir, "missing.pdf"), empty, good} {
		pl := models.PriceList{CompanyID: id, Kind: "file", URL: []string{"null", "missing", "empty", "good"}[i], FilePath: path}
		if err := db.SavePriceList(pl); err != nil {
			t.Fatal(err)
		}
	}
	// A failed refresh must not erase the previous successful download.
	if err := db.SavePriceList(models.PriceList{CompanyID: id, Kind: "file", URL: "good"}); err != nil {
		t.Fatal(err)
	}
	pending, err := db.PendingPriceFiles("yopt")
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("expected 3 pending, got %+v", pending)
	}
	for _, pl := range pending {
		if pl.URL == "good" {
			t.Fatal("existing valid file queued again")
		}
		pl.FilePath = good
		if err := db.SavePriceList(pl); err != nil {
			t.Fatal(err)
		}
	}
	pending, err = db.PendingPriceFiles("yopt")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after repair: %+v, %v", pending, err)
	}
	pending, err = db.PendingPriceFiles("productcenter")
	if err != nil || len(pending) != 0 {
		t.Fatalf("wrong source: %+v, %v", pending, err)
	}
}
