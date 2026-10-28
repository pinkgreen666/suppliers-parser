package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"suppliers-parser/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

type DB struct {
	sql *sql.DB
}

func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return &DB{sql: db}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) Exists(source, url string) (bool, error) {
	var exists bool
	err := d.sql.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM company WHERE source = ? AND url = ?)",
		source, url,
	).Scan(&exists)
	return exists, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (d *DB) SaveCompany(c models.Company) (int64, error) {
	_, err := d.sql.Exec(`
        INSERT INTO company
            (source, url, name, official_name, inn, ogrn,
             address, phone, email, site, telegram, vk, category)
        VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(source, url) DO UPDATE SET
            name          = excluded.name,
            official_name = excluded.official_name,
            inn           = excluded.inn,
            ogrn          = excluded.ogrn,
            address       = excluded.address,
            phone         = excluded.phone,
            email         = excluded.email,
            site          = excluded.site,
            telegram      = excluded.telegram,
            vk            = excluded.vk,
            category      = excluded.category,
            updated       = datetime('now', 'localtime')
    `,
		c.Source, c.URL,
		nullIfEmpty(c.Name),
		nullIfEmpty(c.OfficialName),
		nullIfEmpty(c.INN),
		nullIfEmpty(c.OGRN),
		nullIfEmpty(c.Address),
		nullIfEmpty(c.Phone),
		nullIfEmpty(c.Email),
		nullIfEmpty(c.Site),
		nullIfEmpty(c.Telegram),
		nullIfEmpty(c.VK),
		nullIfEmpty(c.Category),
	)
	if err != nil {
		return 0, err
	}

	var id int64
	err = d.sql.QueryRow(
		"SELECT id FROM company WHERE source = ? AND url = ?",
		c.Source, c.URL,
	).Scan(&id)
	return id, err
}

func (d *DB) SavePriceList(pl models.PriceList) error {
	var itemsJSON any
	if len(pl.Items) > 0 {
		data, err := json.Marshal(pl.Items)
		if err != nil {
			return fmt.Errorf("marshal items: %w", err)
		}
		itemsJSON = string(data)
	}

	_, err := d.sql.Exec(`
        INSERT INTO price_list
            (company_id, kind, url, file_path, items_json)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT (company_id, url) DO UPDATE SET
            company_id = excluded.company_id,
            kind       = excluded.kind,
            url        = excluded.url,
            file_path  = COALESCE(excluded.file_path, price_list.file_path),
            items_json = excluded.items_json
    `,
		pl.CompanyID,
		nullIfEmpty(pl.Kind),
		nullIfEmpty(pl.URL),
		nullIfEmpty(pl.FilePath),
		itemsJSON,
	)

	// var id int64
	// err = db.QueryRow(
	// 	"SELECT id FROM price_list WHERE company_id = ? AND url = ",
	// 	pl.CompanyID, pl.URL,
	// ).Scan(&id)
	// if err != nil {
	// 	fmt.Println(err)
	// }

	return err
}

// PendingPriceFiles includes missing and empty local files, not only NULL paths.
func (d *DB) PendingPriceFiles(source string) ([]models.PriceList, error) {
	rows, err := d.sql.Query(`SELECT p.company_id, p.url, COALESCE(p.file_path, '')
 FROM price_list p JOIN company c ON c.id = p.company_id
 WHERE c.source = ? AND p.kind = 'file' AND p.is_active = 1 AND c.is_active = 1
 AND p.url IS NOT NULL AND p.url != '' ORDER BY p.id`, source)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []models.PriceList
	for rows.Next() {
		p := models.PriceList{Kind: "file"}
		if err := rows.Scan(&p.CompanyID, &p.URL, &p.FilePath); err != nil {
			return nil, err
		}
		info, err := os.Stat(p.FilePath)
		if p.FilePath == "" || os.IsNotExist(err) || (err == nil && (!info.Mode().IsRegular() || info.Size() == 0)) {
			result = append(result, p)
		} else if err != nil {
			return nil, err
		}
	}
	return result, rows.Err()
}
