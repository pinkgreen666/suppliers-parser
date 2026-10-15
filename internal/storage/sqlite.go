package storage

import (
	"database/sql"
	"encoding/json"
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

func SavePriceList(db *sql.DB, pl models.PriceList) (int64, error) {
	var id int64
	err := db.QueryRow(
		"SELECT id FROM price_list WHERE company_id = ? AND url = ?",
		pl.CompanyID, pl.URL,
	).Scan(&id)

	if id == 0 {
		result, err := db.Exec(
			"INSERT INTO price_list (company_id, kind, title, url, file_path, items_json) VALUES (?, ?, ?, ?, ?, ?)",
			pl.CompanyID, pl.Kind, pl.Title, pl.URL, pl.FilePath, pl.ItemsJSON,
		)
		if err != nil {
			return 0, fmt.Errorf("INSERT price_list: %w", err)
		}
		id, _ = result.LastInsertId()
	} else {
		_, err = db.Exec(
			"UPDATE price_list SET kind = ?, title = ?, file_path = ?, items_json = ?, updated = datetime('now', 'localtime') WHERE id = ?",
			pl.Kind, pl.Title, pl.FilePath, pl.ItemsJSON, id,
		)
		if err != nil {
			return 0, fmt.Errorf("UPDATE price_list: %w", err)
		}
	}
	return id, nil
}

func GetPriceListsByCompany(db *sql.DB, companyID int64) ([]models.PriceList, error) {
	rows, err := db.Query(
		"SELECT id, company_id, kind, title, url, file_path, items_json, created, updated FROM price_list WHERE company_id = ? AND is_active = 1",
		companyID,
	)
	if err != nil {
		return nil, fmt.Errorf("SELECT price_list: %w", err)
	}
	defer rows.Close()

	var priceLists []models.PriceList
	for rows.Next() {
		var pl models.PriceList
		var created, updated string
		if err := rows.Scan(&pl.CompanyID, &pl.CompanyID, &pl.Kind, &pl.Title, &pl.URL, &pl.FilePath, &pl.ItemsJSON, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan price_list: %w", err)
		}
		priceLists = append(priceLists, pl)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return priceLists, nil
}

func GetPriceItemsFromJSON(itemsJSON string) ([]models.PriceItem, error) {
	if itemsJSON == "" {
		return nil, nil
	}

	var items []models.PriceItem
	if err := json.Unmarshal([]byte(itemsJSON), &items); err != nil {
		return nil, fmt.Errorf("unmarshal price items: %w", err)
	}
	return items, nil
}
