package runner

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"suppliers-parser/internal/core"
	"suppliers-parser/internal/leadership"
	"suppliers-parser/internal/models"
	"suppliers-parser/internal/storage"
)

type repairSource struct {
	client    *http.Client
	parsed    bool
	companies []models.Company
}

func (s *repairSource) Name() string             { return "yopt" }
func (s *repairSource) HTTPClient() *http.Client { return s.client }
func (s *repairSource) Parse(context.Context) (<-chan models.Company, error) {
	s.parsed = true
	ch := make(chan models.Company, len(s.companies))
	for _, company := range s.companies {
		ch <- company
	}
	close(ch)
	return ch, nil
}

type repairTransport func(*http.Request) (*http.Response, error)

func (f repairTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadsOnlyRepairsExistingCompany(t *testing.T) {
	db := newTestDB(t)
	id, err := db.SaveCompany(models.Company{Source: "yopt", URL: "https://example.test/company"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SavePriceList(models.PriceList{CompanyID: id, Kind: "file", URL: "https://example.test/price.pdf"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	src := &repairSource{client: &http.Client{Transport: repairTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("%PDF-complete"))}, nil
	})}}
	runner := New(db).WithDownloadsOnly(true)
	stats := runner.RunSources(context.Background(), []core.Source{src})["yopt"]
	if src.parsed || calls != 1 || stats.SavedPrices != 1 || stats.Failed != 0 {
		t.Fatalf("parsed=%v calls=%d stats=%+v", src.parsed, calls, stats)
	}
	pending, err := db.PendingPriceFiles("yopt")
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	runner.RunSources(context.Background(), []core.Source{src})
	if calls != 1 {
		t.Fatalf("downloaded existing file again: %d calls", calls)
	}
}

func newTestDB(t *testing.T) *storage.DB {
	t.Helper()
	migrations, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil || len(migrations) == 0 {
		t.Fatalf("migrations: %v", err)
	}
	var schema []string
	for _, path := range migrations {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		schema = append(schema, string(b))
	}
	t.Chdir(t.TempDir())
	sqlDB, err := sql.Open("sqlite3", "test.db")
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range schema {
		if _, err := sqlDB.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	sqlDB.Close()
	db, err := storage.Open("test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db

}

func TestForceUpdatesCompaniesAndPricesWithoutRepairPass(t *testing.T) {
	db := newTestDB(t)
	id, err := db.SaveCompany(models.Company{Source: "yopt", URL: "https://example.test/company", Name: "old", Phone: "old", Email: "old"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pl := range []models.PriceList{
		{CompanyID: id, Kind: "file", URL: "https://example.test/stale.pdf"},
		{CompanyID: id, Kind: "inline", URL: "https://example.test/items", Items: []models.PriceItem{{Name: "old", Price: "1"}}},
	} {
		if err := db.SavePriceList(pl); err != nil {
			t.Fatal(err)
		}
	}
	fresh := models.Company{Source: "yopt", URL: "https://example.test/company", Name: "new", OfficialName: "official", INN: "123", OGRN: "456", Category: "category", Address: "address", Phone: "phone", Email: "", Site: "site", Telegram: "tg", VK: "vk", PriceLists: []models.PriceList{
		{Kind: "inline", URL: "https://example.test/items", Items: []models.PriceItem{{Name: "new", Price: "2"}}},
		{Kind: "file", URL: "https://example.test/fresh.pdf"},
	}}
	src := &repairSource{companies: []models.Company{fresh, {Source: "yopt", URL: "https://example.test/new-company", Name: "created"}}}
	calls := 0
	src.client = &http.Client{Transport: repairTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if !src.parsed || r.URL.Path != "/fresh.pdf" {
			t.Errorf("unexpected preliminary repair request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("%PDF-new"))}, nil
	})}
	stats, err := New(db).WithForce(true).WithDelay(0).runSource(context.Background(), src)
	if err != nil || stats.SavedCompanies != 2 || stats.Skipped != 0 || stats.Failed != 0 || calls != 1 {
		t.Fatalf("stats=%+v calls=%d err=%v", stats, calls, err)
	}
	queryDB, err := sql.Open("sqlite3", "test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer queryDB.Close()
	var got models.Company
	var gotID int64
	err = queryDB.QueryRow(`SELECT id, name, official_name, inn, ogrn, category, address, phone, COALESCE(email,''), site, telegram, vk FROM company WHERE url=?`, fresh.URL).Scan(&gotID, &got.Name, &got.OfficialName, &got.INN, &got.OGRN, &got.Category, &got.Address, &got.Phone, &got.Email, &got.Site, &got.Telegram, &got.VK)
	if err != nil {
		t.Fatal(err)
	}
	want := fresh
	want.Source = ""
	want.URL = ""
	want.PriceLists = nil
	if gotID != id || !reflect.DeepEqual(got, want) {
		t.Fatalf("company id=%d got=%+v want=%+v", gotID, got, want)
	}
	var count int
	if err := queryDB.QueryRow(`SELECT count(*) FROM company`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("company count=%d err=%v", count, err)
	}
	var items string
	if err := queryDB.QueryRow(`SELECT items_json FROM price_list WHERE company_id=? AND url=?`, id, "https://example.test/items").Scan(&items); err != nil {
		t.Fatal(err)
	}
	var parsed []models.PriceItem
	if err := json.Unmarshal([]byte(items), &parsed); err != nil || !reflect.DeepEqual(parsed, fresh.PriceLists[0].Items) {
		t.Fatalf("items=%s err=%v", items, err)
	}
}

func TestEnrichmentRunsBeforeDownloadAndForExisting(t *testing.T) {
	db := newTestDB(t)
	calls := 0
	fileCalls := 0
	src := &repairSource{companies: []models.Company{{Source: "yopt", URL: "https://example.test/c", Name: "Company", PriceLists: []models.PriceList{{Kind: "file", URL: "https://example.test/p.pdf"}}}}}
	src.client = &http.Client{Transport: repairTransport(func(r *http.Request) (*http.Response, error) {
		if calls != 1 {
			t.Errorf("download before enrichment: calls=%d", calls)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("%PDF-test"))}, nil
	})}
	r := New(db).WithDelay(0).WithEnrichment(func(ctx context.Context, c leadership.Company) error {
		calls++
		pending, err := db.PendingPriceFiles("yopt")
		if err != nil {
			return err
		}
		if calls == 1 && len(pending) != 1 {
			t.Errorf("URLs not saved before enrichment: %v", pending)
		}
		return nil
	})
	r.WithFileEnrichment(func(ctx context.Context, id int64) error {
		fileCalls++
		pending, err := db.PendingPriceFiles("yopt")
		if err != nil {
			return err
		}
		if len(pending) != 0 {
			t.Fatal("file hook before successful download")
		}
		return nil
	})
	r.RunSources(context.Background(), []core.Source{src})
	r.RunSources(context.Background(), []core.Source{src})
	if fileCalls != 1 {
		t.Fatalf("file calls=%d", fileCalls)
	}
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
}
