package enrichment

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"suppliers-parser/internal/categorize"
	"suppliers-parser/internal/leadership"
	"suppliers-parser/internal/models"
	"suppliers-parser/internal/storage"
)

type fakeProvider struct {
	calls   int
	failure error
}

func (p *fakeProvider) Lookup(ctx context.Context, id leadership.Identifier) (leadership.Result, error) {
	p.calls++
	return leadership.Result{Status: "found", INN: id.Value, CheckedAt: time.Now(), Leaders: []leadership.Leader{{Name: "Иванов Иван Иванович", Post: "Директор", Type: "PERSON"}}}, p.failure
}
func TestServiceSharedCacheAndStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(string(b)); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()
	db, err := storage.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := categorize.NewMatcher([]categorize.Category{{ID: 1, Name: "Ноутбуки"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	provider := &fakeProvider{}
	s := &Service{DB: db, Matcher: m, Provider: provider}
	ctx := context.Background()
	for _, source := range []string{"yopt", "productcenter"} {
		id, err := db.SaveCompany(models.Company{Source: source, URL: "https://example.test/c", INN: "7719402047"})
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SavePriceList(models.PriceList{CompanyID: id, Kind: "inline", URL: "https://example.test/p", Items: []models.PriceItem{{Name: "Ноутбук"}}}); err != nil {
			t.Fatal(err)
		}
		c, err := db.LeadershipCompany(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Process(ctx, c); err != nil {
			t.Fatal(err)
		}
	}
	if provider.calls != 1 {
		t.Fatalf("duplicate lookup: %d", provider.calls)
	}
	provider.failure = &leadership.APIError{Status: 429}
	id, err := db.SaveCompany(models.Company{Source: "yopt", URL: "https://example.test/other", INN: "7707083893"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := db.LeadershipCompany(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	s.next = time.Time{}
	if err := s.Process(ctx, c); err == nil {
		t.Fatal("expected rate limit error")
	}
	if !s.stopped {
		t.Fatal("lookup was not stopped")
	}
	if err := s.Process(ctx, c); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 2 {
		t.Fatalf("request after stop: %d", provider.calls)
	}
}
