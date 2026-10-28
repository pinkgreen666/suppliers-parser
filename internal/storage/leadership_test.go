package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"suppliers-parser/internal/leadership"
	"suppliers-parser/internal/models"
)

type fakeLeadership struct {
	calls   int
	failure error
}

func (p *fakeLeadership) Lookup(ctx context.Context, id leadership.Identifier) (leadership.Result, error) {
	p.calls++
	if p.failure != nil {
		return leadership.Result{}, p.failure
	}
	return leadership.Result{Status: "found", INN: "7719402047", OGRN: "1157746078984", EntityType: "LEGAL", Source: "dadata", CheckedAt: time.Now().UTC(), Leaders: []leadership.Leader{{Name: "Иванов Иван Иванович", Type: "PERSON", Post: "ДИРЕКТОР"}}}, nil
}
func leadershipDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	migrations, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range migrations {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.sql.Exec(string(b)); err != nil {
			t.Fatal(err)
		}
	}
	for _, source := range []string{"yopt", "productcenter"} {
		if _, err := db.SaveCompany(models.Company{Source: source, Name: "Компания", URL: "https://example.test/" + source, INN: "7719402047", OGRN: "1157746078984"}); err != nil {
			t.Fatal(err)
		}
	}
	return db
}
func TestLeadershipCacheAcrossAdaptersAndPreserveOnError(t *testing.T) {
	db := leadershipDB(t)
	ctx := context.Background()
	if err := db.CheckLeadershipSchema(ctx); err != nil {
		t.Fatal(err)
	}
	companies, err := db.LeadershipCompanies(ctx, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	opts := leadership.Options{TTL: 30 * 24 * time.Hour, RetryAfter: time.Hour}
	provider := &fakeLeadership{}
	report, err := leadership.Run(ctx, db, provider, companies, opts)
	if err != nil || provider.calls != 1 || len(report.Records) != 2 || !report.Records[1].Cached {
		t.Fatalf("calls=%d report=%+v err=%v", provider.calls, report, err)
	}
	if _, err := leadership.Run(ctx, db, provider, companies, opts); err != nil || provider.calls != 1 {
		t.Fatal("cache not reused", err)
	}
	opts.Force = true
	provider.failure = errors.New("temporary failure")
	report, err = leadership.Run(ctx, db, provider, companies, opts)
	if err != nil || provider.calls != 2 {
		t.Fatalf("calls=%d err=%v", provider.calls, err)
	}
	for _, record := range report.Records {
		if record.Status != "error" || record.Result == nil || record.Result.Leaders[0].Name != "Иванов Иван Иванович" {
			t.Fatalf("lost previous data: %+v", record)
		}
	}
	opts.Force = false
	if _, err := leadership.Run(ctx, db, provider, companies, opts); err != nil || provider.calls != 2 {
		t.Fatal("retry cooldown ignored", err)
	}
	// Age the failed attempt so the next regular run retries it.
	if _, err := db.sql.Exec(`UPDATE leadership_cache SET attempted_at=?`, time.Now().Add(-2*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	provider.failure = nil
	if _, err := leadership.Run(ctx, db, provider, companies, opts); err != nil || provider.calls != 3 {
		t.Fatal("did not retry", err)
	}
	// Expiration also refreshes a successful lookup.
	cache, err := db.GetCache(ctx, "inn:7719402047")
	if err != nil {
		t.Fatal(err)
	}
	cache.Result.CheckedAt = time.Now().Add(-31 * 24 * time.Hour)
	if err := db.PutCache(ctx, "inn:7719402047", cache); err != nil {
		t.Fatal(err)
	}
	if _, err := leadership.Run(ctx, db, provider, companies, opts); err != nil || provider.calls != 4 {
		t.Fatal("TTL ignored", err)
	}
}

func TestLeadershipInvalidMismatchDryRunAndStop(t *testing.T) {
	db := leadershipDB(t)
	ctx := context.Background()
	companies, _ := db.LeadershipCompanies(ctx, nil, 0)
	opts := leadership.Options{TTL: time.Hour, RetryAfter: time.Hour}
	provider := &fakeLeadership{}
	companies[0].INN = "bad"
	companies[0].OGRN = "bad"
	report, err := leadership.Run(ctx, db, provider, companies[:1], opts)
	if err != nil || provider.calls != 0 || report.Records[0].Status != "invalid_identifier" {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	companies, _ = db.LeadershipCompanies(ctx, []string{"productcenter"}, 1)
	if len(companies) != 1 || companies[0].Source != "productcenter" {
		t.Fatal("source filter")
	}
	companies[0].OGRN = "1027700132195" // Valid OGRN of another entity.
	report, err = leadership.Run(ctx, db, provider, companies, opts)
	if err != nil || report.Records[0].Status != "needs_review" || report.Records[0].Result != nil {
		t.Fatalf("mismatch report=%+v err=%v", report, err)
	}
	opts.DryRun = true
	report, err = leadership.Run(ctx, nil, nil, companies, opts)
	if err != nil || report.Requests != 0 || report.Records[0].Status != "pending" {
		t.Fatalf("dry-run=%+v err=%v", report, err)
	}
	opts.DryRun = false
	opts.Force = true
	companies, _ = db.LeadershipCompanies(ctx, nil, 0)
	provider.failure = &leadership.APIError{Status: 429, RetryAfter: "60"}
	report, err = leadership.Run(ctx, db, provider, companies, opts)
	if err == nil || len(report.Records) != 1 || report.Requests != 1 {
		t.Fatalf("did not stop: %+v %v", report, err)
	}
}

func TestLeadershipMigrationDown(t *testing.T) {
	db := leadershipDB(t)
	down, err := os.ReadFile("../../migrations/000004_company_leadership.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(string(down)); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckLeadershipSchema(context.Background()); err == nil {
		t.Fatal("missing migration not detected")
	}
	var count int
	if err := db.sql.QueryRow(`SELECT count(*) FROM company`).Scan(&count); err != nil || count != 2 {
		t.Fatal("company data affected", err)
	}
}
