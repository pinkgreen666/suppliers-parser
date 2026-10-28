package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"suppliers-parser/internal/leadership"
)

func (d *DB) LeadershipCompanies(ctx context.Context, sources []string, limit int) ([]leadership.Company, error) {
	query := `SELECT id,source,COALESCE(name,''),COALESCE(inn,''),COALESCE(ogrn,'') FROM company WHERE is_active=1`
	var args []any
	if len(sources) > 0 {
		query += " AND source IN (" + strings.TrimRight(strings.Repeat("?,", len(sources)), ",") + ")"
		for _, s := range sources {
			args = append(args, s)
		}
	}
	query += " ORDER BY id"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := d.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []leadership.Company
	for rows.Next() {
		var c leadership.Company
		if err := rows.Scan(&c.ID, &c.Source, &c.Name, &c.INN, &c.OGRN); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
func (d *DB) CheckLeadershipSchema(ctx context.Context) error {
	for _, query := range []string{`SELECT lookup_key,result_json,checked_at,attempted_at,last_error FROM leadership_cache LIMIT 0`, `SELECT company_id,lookup_key,status,result_json,attempted_at,last_error FROM company_leadership LIMIT 0`} {
		rows, err := d.sql.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("нужна миграция 000004: выполните make migrate-up: %w", err)
		}
		rows.Close()
	}
	return nil
}
func (d *DB) GetCache(ctx context.Context, key string) (leadership.Cache, error) {
	var cache leadership.Cache
	var raw sql.NullString
	var attempted int64
	err := d.sql.QueryRowContext(ctx, `SELECT result_json,attempted_at,last_error FROM leadership_cache WHERE lookup_key=?`, key).Scan(&raw, &attempted, &cache.Error)
	if err == sql.ErrNoRows {
		return cache, nil
	}
	if err != nil {
		return cache, err
	}
	cache.AttemptedAt = time.Unix(attempted, 0).UTC()
	if raw.Valid {
		if err := json.Unmarshal([]byte(raw.String), &cache.Result); err != nil {
			return cache, err
		}
	}
	return cache, nil
}
func (d *DB) PutCache(ctx context.Context, key string, c leadership.Cache) error {
	var raw any
	var checked any
	if c.Result != nil {
		b, err := json.Marshal(c.Result)
		if err != nil {
			return err
		}
		raw = string(b)
		checked = c.Result.CheckedAt.Unix()
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO leadership_cache(lookup_key,result_json,checked_at,attempted_at,last_error) VALUES(?,?,?,?,?)
 ON CONFLICT(lookup_key) DO UPDATE SET
 result_json=CASE WHEN excluded.last_error<>'' THEN leadership_cache.result_json ELSE excluded.result_json END,
 checked_at=CASE WHEN excluded.last_error<>'' THEN leadership_cache.checked_at ELSE excluded.checked_at END,
 attempted_at=excluded.attempted_at,last_error=excluded.last_error`, key, raw, checked, c.AttemptedAt.Unix(), c.Error)
	return err
}
func (d *DB) SaveRecord(ctx context.Context, r leadership.Record) (leadership.Record, error) {
	if r.Status == "error" && r.Result == nil {
		var old sql.NullString
		err := d.sql.QueryRowContext(ctx, `SELECT result_json FROM company_leadership WHERE company_id=? AND lookup_key=?`, r.Company.ID, r.Key).Scan(&old)
		if err != nil && err != sql.ErrNoRows {
			return r, err
		}
		if old.Valid {
			if err := json.Unmarshal([]byte(old.String), &r.Result); err != nil {
				return r, err
			}
			if r.Result != nil && !leadership.MatchesCompany(r.Company, *r.Result) {
				r.Result = nil
			}
		}
	}
	var raw any
	if r.Result != nil {
		b, err := json.Marshal(r.Result)
		if err != nil {
			return r, err
		}
		raw = string(b)
	}
	_, err := d.sql.ExecContext(ctx, `INSERT INTO company_leadership(company_id,lookup_key,status,result_json,attempted_at,last_error) VALUES(?,?,?,?,?,?)
 ON CONFLICT(company_id) DO UPDATE SET lookup_key=excluded.lookup_key,status=excluded.status,result_json=excluded.result_json,attempted_at=excluded.attempted_at,last_error=excluded.last_error`, r.Company.ID, r.Key, r.Status, raw, r.AttemptedAt.Unix(), r.Error)
	return r, err
}
