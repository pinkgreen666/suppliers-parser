package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"log"

	"suppliers-parser/internal/models"

	_ "github.com/mattn/go-sqlite3"
	"github.com/xuri/excelize/v2"
)

func main() {
	dbPath := flag.String("db", "data.db", "путь к SQLite")
	outPath := flag.String("out", "companies.xlsx", "выходной Excel")
	source := flag.String("source", "", "фильтр по источнику (пусто = все)")
	flag.Parse()

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("открытие БД: %v", err)
	}
	defer db.Close()

	f := excelize.NewFile()
	defer f.Close()

	// Лист 1: Компании
	if err := writeCompanies(f, db, *source); err != nil {
		log.Fatalf("компании: %v", err)
	}

	// Лист 2: Прайс-листы
	if err := writePriceLists(f, db, *source); err != nil {
		log.Fatalf("прайс-листы: %v", err)
	}

	// Лист 3: Товары из inline-прайсов
	if err := writePriceItems(f, db, *source); err != nil {
		log.Fatalf("товары: %v", err)
	}

	// Удаляем дефолтный Sheet1, если он остался пустым
	if f.SheetCount > 1 {
		f.DeleteSheet("Sheet1")
	}

	if err := f.SaveAs(*outPath); err != nil {
		log.Fatalf("сохранение: %v", err)
	}
	log.Printf("готово: %s", *outPath)
}

// ---------- Лист "Компании" ----------

func writeCompanies(f *excelize.File, db *sql.DB, source string) error {
	sheet := "Компании"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}

	headers := []string{
		"ID", "Источник", "Название", "Официальное название",
		"ИНН", "ОГРН", "Категория", "Адрес", "Телефон",
		"Email", "Сайт", "Телеграм", "ВК", "YouTube",
		"Есть прайс", "Ссылка", "Создано", "Обновлено",
	}
	writeHeader(f, sheet, headers)

	query := `
		SELECT c.id, c.source, c.name, c.official_name, c.inn, c.ogrn, c.category,
		       c.address, c.phone, c.email, c.site, c.telegram, c.vk, c.youtube,
		       c.url, c.created, c.updated,
		       (SELECT COUNT(*) FROM price_list p WHERE p.company_id = c.id) AS prices_count
		FROM company c
		WHERE c.is_active = 1
	`
	var args []any
	if source != "" {
		query += " AND c.source = ?"
		args = append(args, source)
	}
	query += " ORDER BY c.id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	row := 2
	for rows.Next() {
		var (
			id                                  int64
			src, name, official, inn, ogrn, cat sql.NullString
			address, phone, email, site         sql.NullString
			telegram, vk, youtube, url          sql.NullString
			created, updated                    sql.NullString
			pricesCount                         int
		)
		if err := rows.Scan(&id, &src, &name, &official, &inn, &ogrn, &cat,
			&address, &phone, &email, &site, &telegram, &vk, &youtube,
			&url, &created, &updated, &pricesCount); err != nil {
			return err
		}

		hasPrice := "нет"
		if pricesCount > 0 {
			hasPrice = "да"
		}

		values := []any{
			id, src.String, name.String, official.String,
			inn.String, ogrn.String, cat.String, address.String, phone.String,
			email.String, site.String, telegram.String, vk.String, youtube.String,
			hasPrice, url.String, created.String, updated.String,
		}
		writeRow(f, sheet, row, values)

		// Кликабельная ссылка на карточку
		if url.String != "" {
			cell, _ := excelize.CoordinatesToCellName(16, row) // колонка "Ссылка"
			f.SetCellHyperLink(sheet, cell, url.String, "External")
		}
		row++
	}
	return rows.Err()
}

// ---------- Лист "Прайс-листы" ----------

func writePriceLists(f *excelize.File, db *sql.DB, source string) error {
	sheet := "Прайс-листы"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}

	headers := []string{
		"ID", "Компания", "ИНН", "Тип", "Название",
		"URL", "Локальный файл", "Активен", "Создан", "Обновлён",
	}
	writeHeader(f, sheet, headers)

	query := `
		SELECT p.id, c.name, c.inn, p.kind, p.url, p.file_path,
		       p.is_active, p.created, p.updated
		FROM price_list p
		JOIN company c ON c.id = p.company_id
		WHERE p.is_active = 1
	`
	var args []any
	if source != "" {
		query += " AND c.source = ?"
		args = append(args, source)
	}
	query += " ORDER BY p.company_id, p.id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	row := 2
	for rows.Next() {
		var (
			id                 int64
			company, inn, kind sql.NullString
			url, filePath      sql.NullString
			created, updated   sql.NullString
			isActive           int
		)
		if err := rows.Scan(&id, &company, &inn, &kind, &url, &filePath,
			&isActive, &created, &updated); err != nil {
			return err
		}

		values := []any{
			id, company.String, inn.String, kind.String, url.String,
			filePath.String, isActive, created.String, updated.String,
		}
		writeRow(f, sheet, row, values)

		if url.String != "" {
			cell, _ := excelize.CoordinatesToCellName(6, row) // колонка "URL"
			f.SetCellHyperLink(sheet, cell, url.String, "External")
		}
		row++
	}
	return rows.Err()
}

// ---------- Лист "Товары" ----------

func writePriceItems(f *excelize.File, db *sql.DB, source string) error {
	sheet := "Товары"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}

	headers := []string{
		"Компания", "ИНН", "Название товара", "Цена",
		"Категория товара", "Описание", "Ссылка на товар",
	}
	writeHeader(f, sheet, headers)

	query := `
		SELECT c.name, c.inn, p.items_json
		FROM price_list p
		JOIN company c ON c.id = p.company_id
		WHERE p.kind = 'inline' AND p.items_json IS NOT NULL AND p.items_json != ''
	`
	var args []any
	if source != "" {
		query += " AND c.source = ?"
		args = append(args, source)
	}
	query += " ORDER BY c.id"

	rows, err := db.Query(query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	row := 2
	for rows.Next() {
		var company, inn, itemsJSON sql.NullString
		if err := rows.Scan(&company, &inn, &itemsJSON); err != nil {
			return err
		}
		if itemsJSON.String == "" {
			continue
		}

		var items []models.PriceItem
		if err := json.Unmarshal([]byte(itemsJSON.String), &items); err != nil {
			log.Printf("битый JSON у %s: %v", company.String, err)
			continue
		}

		for _, it := range items {
			values := []any{
				company.String, inn.String, it.Name, it.Price,
				it.Category, it.Desc, it.URL,
			}
			writeRow(f, sheet, row, values)

			if it.URL != "" {
				cell, _ := excelize.CoordinatesToCellName(7, row)
				f.SetCellHyperLink(sheet, cell, it.URL, "External")
			}
			row++
		}
	}
	return rows.Err()
}

// ---------- Хелперы ----------

func writeHeader(f *excelize.File, sheet string, headers []string) {
	style, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"#DDDDDD"},
			Pattern: 1,
		},
	})

	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		f.SetCellValue(sheet, cell, h)
		f.SetCellStyle(sheet, cell, cell, style)
	}

	// Автоширина
	for i, h := range headers {
		col, _ := excelize.ColumnNumberToName(i + 1)
		f.SetColWidth(sheet, col, col, float64(max(len(h)+2, 12)))
	}
}

func writeRow(f *excelize.File, sheet string, row int, values []any) {
	for i, v := range values {
		cell, _ := excelize.CoordinatesToCellName(i+1, row)
		f.SetCellValue(sheet, cell, v)
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
