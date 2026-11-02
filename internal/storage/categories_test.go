package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"suppliers-parser/internal/categorize"
	"suppliers-parser/internal/models"
)

func TestCategoryPersistenceAndIsolation(t *testing.T) {
	db := leadershipDB(t)
	ctx := context.Background()
	companies, err := db.LeadershipCompanies(ctx, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	m, err := categorize.NewMatcher([]categorize.Category{{ID: 1, Name: "Ноутбуки"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := companies[0].ID
	if err := db.SavePriceList(models.PriceList{CompanyID: id, Kind: "inline", URL: "https://example.test/products", Items: []models.PriceItem{{Name: "Ноутбук Lenovo"}}}); err != nil {
		t.Fatal(err)
	}
	r, err := db.CategorizeCompany(ctx, id, m)
	if err != nil || len(r.Categories) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	var raw string
	if err := db.sql.QueryRow(`SELECT category_normal_json FROM company WHERE id=?`, id).Scan(&raw); err != nil || raw != `["Ноутбуки"]` {
		t.Fatalf("%s %v", raw, err)
	}
	r, err = db.CategorizeCompany(ctx, companies[1].ID, m)
	if err != nil || len(r.Categories) != 0 || r.Status != "insufficient_data" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := db.sql.Exec(`UPDATE price_list SET items_json='broken' WHERE company_id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CategorizeCompany(ctx, id, m); err == nil {
		t.Fatal("expected malformed JSON error")
	}
	if err := db.sql.QueryRow(`SELECT category_normal_json FROM company WHERE id=?`, id).Scan(&raw); err != nil || raw != `["Ноутбуки"]` {
		t.Fatalf("lost previous result: %s %v", raw, err)
	}
}

func TestFileExtractionCacheInvalidationAndDeduplication(t *testing.T) {
	db := leadershipDB(t)
	ctx := context.Background()
	companies, err := db.LeadershipCompanies(ctx, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	id := companies[0].ID
	path := filepath.Join(t.TempDir(), "price.csv")
	if err := os.WriteFile(path, []byte("Наименование;Цена\nНоутбук;100\nМонитор;50\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePriceList(models.PriceList{CompanyID: id, Kind: "inline", URL: "inline", Items: []models.PriceItem{{Name: "Ноутбук"}}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SavePriceList(models.PriceList{CompanyID: id, Kind: "file", URL: "file", FilePath: path}); err != nil {
		t.Fatal(err)
	}
	c, err := db.CategoryCompany(ctx, id)
	if err != nil || len(c.Products) != 2 {
		t.Fatalf("%+v %v", c, err)
	}
	if c.Products[1].SourceFile != path {
		t.Fatalf("missing provenance: %+v", c.Products)
	}
	var old string
	if err := db.sql.QueryRow(`SELECT fingerprint FROM price_file_extraction`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Наименование;Цена\nПринтер;100\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err = db.CategoryCompany(ctx, id)
	if err != nil || len(c.Products) != 2 || c.Products[1].Name != "Принтер" {
		t.Fatalf("%+v %v", c, err)
	}
	var fresh string
	db.sql.QueryRow(`SELECT fingerprint FROM price_file_extraction`).Scan(&fresh)
	if fresh == old {
		t.Fatal("cache not refreshed")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	c, err = db.CategoryCompany(ctx, id)
	if err != nil || len(c.Products) != 1 || len(c.Warnings) != 1 {
		t.Fatalf("missing file breaks inline: %+v %v", c, err)
	}
}
