package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"log"
	"strings"
	"suppliers-parser/internal/leadership"

	"suppliers-parser/internal/models"

	_ "github.com/mattn/go-sqlite3"
	"github.com/xuri/excelize/v2"
)

func main() {
	dbPath := flag.String("db", "data.db", "путь к SQLite")
	outPath := flag.String("out", "companies.xlsx", "выходной Excel")
	source := flag.String("source", "", "фильтр по источнику (пусто = все)")
	flag.Parse()
	log.Print("[export] фильтр: активные компании с is_russian=1; перед экспортом выполните geography")

	db, err := sql.Open("sqlite3", *dbPath)
	if err != nil {
		log.Fatalf("[export] открытие БД: %v", err)
	}
	defer db.Close()

	f := excelize.NewFile()
	defer f.Close()

	// Лист 1: Компании
	if err := writeCompanies(f, db, *source); err != nil {
		log.Fatalf("[export] компании: %v", err)
	}

	// Лист 2: Прайс-листы
	if err := writePriceLists(f, db, *source); err != nil {
		log.Fatalf("[export] прайс-листы: %v", err)
	}

	// Лист 3: Товары из inline-прайсов
	if err := writePriceItems(f, db, *source); err != nil {
		log.Fatalf("[export] товары: %v", err)
	}

	// Удаляем дефолтный Sheet1, если он остался пустым
	if f.SheetCount > 1 {
		f.DeleteSheet("Sheet1")
	}

	if err := f.SaveAs(*outPath); err != nil {
		log.Fatalf("[export] сохранение: %v", err)
	}
	log.Printf("[export] готово: %s", *outPath)
}

// ---------- Лист "Компании" ----------

func writeCompanies(f *excelize.File, db *sql.DB, source string) error {
	sheet := "Компании"
	if _, err := f.NewSheet(sheet); err != nil {
		return err
	}

	headers := []string{"Источник", "Название", "Официальное название", "ИНН", "ОГРН", "Категория", "Нормальные категории", "Руководитель", "Должность руководителя", "Адрес", "Телефон", "Email", "Сайт", "Есть прайс", "Ссылка"}
	writeHeader(f, sheet, headers)
	query := `SELECT c.source,c.name,c.official_name,c.inn,c.ogrn,c.category,c.category_normal_json,
 l.result_json,c.address,c.phone,c.email,c.site,c.url,
 (SELECT COUNT(*) FROM price_list p WHERE p.company_id=c.id AND p.is_active=1)
 FROM company c LEFT JOIN company_leadership l ON l.company_id=c.id WHERE c.is_active=1 AND c.is_russian=1`

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
		var src, name, official, inn, ogrn, cat, normal, leader, address, phone, email, site, url sql.NullString
		var pricesCount int
		if err := rows.Scan(&src, &name, &official, &inn, &ogrn, &cat, &normal, &leader, &address, &phone, &email, &site, &url, &pricesCount); err != nil {
			return err
		}
		var categories []string
		if err := json.Unmarshal([]byte(normal.String), &categories); err != nil {
			return err
		}
		officialName := strings.TrimSpace(official.String)
		var names, posts []string
		if leader.Valid {
			var result leadership.Result
			if err := json.Unmarshal([]byte(leader.String), &result); err != nil {
				return err
			}
			if leadership.MatchesCompany(leadership.Company{INN: inn.String, OGRN: ogrn.String}, result) {
				if companyName := strings.TrimSpace(result.CompanyName); companyName != "" {
					officialName = companyName
				}
				for _, person := range result.Leaders {
					names = append(names, person.Name)
					posts = append(posts, person.Post)
				}
			}
		}
		hasPrice := "нет"
		if pricesCount > 0 {
			hasPrice = "да"
		}
		values := []any{src.String, name.String, officialName, inn.String, ogrn.String, cat.String, strings.Join(categories, "; "), strings.Join(names, "\n"), strings.Join(posts, "\n"), address.String, phone.String, email.String, site.String, hasPrice, url.String}

		writeRow(f, sheet, row, values)

		// Кликабельная ссылка на карточку
		if url.String != "" {
			cell, _ := excelize.CoordinatesToCellName(15, row) // колонка "Ссылка"
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

	headers := []string{"Компания", "ИНН", "Тип", "URL", "Локальный файл"}
	writeHeader(f, sheet, headers)
	query := `SELECT c.name,c.inn,p.kind,p.url,p.file_path FROM price_list p JOIN company c ON c.id=p.company_id WHERE p.is_active=1 AND c.is_active=1 AND c.is_russian=1`

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
		var company, inn, kind, url, filePath sql.NullString
		if err := rows.Scan(&company, &inn, &kind, &url, &filePath); err != nil {
			return err
		}
		values := []any{company.String, inn.String, kind.String, url.String, filePath.String}

		writeRow(f, sheet, row, values)

		if url.String != "" {
			cell, _ := excelize.CoordinatesToCellName(4, row) // колонка "URL"
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
		WHERE c.is_active=1 AND c.is_russian=1 AND p.is_active=1 AND p.kind = 'inline' AND p.items_json IS NOT NULL AND p.items_json != ''
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
			log.Printf("[export] битый JSON у %s: %v", company.String, err)
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
