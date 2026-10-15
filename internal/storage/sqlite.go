package storage

import (
	"database/sql"

	"suppliers-parser/internal/models"

	_ "github.com/mattn/go-sqlite3"
)

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	_, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS company (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			source        TEXT NOT NULL,
			url           TEXT NOT NULL,
			name          TEXT,
			official_name TEXT,
			inn           TEXT,
			ogrn          TEXT,
			address       TEXT,
			phone         TEXT,
			email         TEXT,
			site          TEXT,
			telegram      TEXT,
			vk            TEXT,
			youtube       TEXT,
			is_active     INTEGER NOT NULL DEFAULT 1,
			created       TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
			updated       TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
			UNIQUE(source, url)
        );
        CREATE INDEX IF NOT EXISTS idx_company_inn ON company(inn);
    `)
	if err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func Save(db *sql.DB, c models.Company) error {
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
	return err
}

func Exists(db *sql.DB, source, url string) (bool, error) {
	var exists bool
	err := db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM company WHERE source = ? AND url = ?)",
		source, url,
	).Scan(&exists)
	return exists, err
}
