package storage

import (
	"database/sql"
	"fmt"

	"suppliers-parser/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

func Open(path string) (*sql.DB, error) {
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

	return db, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func Save(db *sql.DB, c models.Company) (int64, error) {
	_, err := db.Exec(`
        INSERT INTO company
            (source, url, name, official_name, inn, ogrn,
             address, phone, email, site, telegram, vk, youtube)
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
            youtube       = excluded.youtube,
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
		nullIfEmpty(c.Youtube),
	)

	var id int64
	err = db.QueryRow(
		"SELECT id FROM company WHERE source = ? AND url = ?",
		c.Source, c.URL,
	).Scan(&id)
	fmt.Println(id)
	return id, err
}

func Exists(db *sql.DB, source, url string) (bool, error) {
	var exists bool
	err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM company WHERE source = ? AND url = ?)",
		source, url,
	).Scan(&exists)
	return exists, err
}

func SavePriceList(db *sql.DB, pl models.PriceList) error {
	_, err := db.Exec(`
        INSERT INTO price_list
            (company_id, kind, url, file_path, items_json)
        VALUES (?, ?, ?, ?, ?)
        ON CONFLICT DO UPDATE SET
            company_id = excluded.company_id,
            kind       = excluded.kind,
            url        = excluded.url,
            file_path  = excluded.file_path,
            items_json = excluded.items_json
    `,
		pl.CompanyID,
		nullIfEmpty(pl.Kind),
		nullIfEmpty(pl.URL),
		nullIfEmpty(pl.FilePath),
		nullIfEmpty(pl.ItemsJSON),
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
