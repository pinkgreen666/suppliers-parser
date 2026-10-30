package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"suppliers-parser/internal/geography"
	"suppliers-parser/internal/leadership"
)

type GeographyRecord struct {
	ID     int64  `json:"company_id"`
	Source string `json:"source"`
	Name   string `json:"name"`
	geography.Decision
}

// ClassifyGeography takes a consistent local snapshot and persists it atomically.
func (d *DB) ClassifyGeography(ctx context.Context) ([]GeographyRecord, error) {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT c.id,c.source,COALESCE(c.name,''),COALESCE(c.inn,''),COALESCE(c.ogrn,''),COALESCE(c.address,''),l.result_json FROM company c LEFT JOIN company_leadership l ON l.company_id=c.id WHERE c.is_active=1 ORDER BY c.id`)
	if err != nil {
		return nil, err
	}
	var records []GeographyRecord
	for rows.Next() {
		var r GeographyRecord
		var e geography.Evidence
		var raw sql.NullString
		if err := rows.Scan(&r.ID, &r.Source, &r.Name, &e.INN, &e.OGRN, &e.Address, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		if raw.Valid {
			var registry leadership.Result
			if err := json.Unmarshal([]byte(raw.String), &registry); err != nil {
				rows.Close()
				return nil, fmt.Errorf("company_id=%d: result_json: %w", r.ID, err)
			}
			e.Registry = &registry
		}
		r.Decision = geography.Classify(e)
		records = append(records, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, r := range records {
		var value any
		if r.IsRussian != nil {
			value = *r.IsRussian
		}
		if _, err := tx.ExecContext(ctx, `UPDATE company SET is_russian=? WHERE id=?`, value, r.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return records, nil
}
