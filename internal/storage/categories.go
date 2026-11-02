package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"suppliers-parser/internal/categorize"
	"suppliers-parser/internal/models"
)

// CategoryCompany reads products by company ID, avoiding ambiguous name joins.
func (d *DB) CategoryCompany(ctx context.Context, id int64) (categorize.Company, error) {
	c := categorize.Company{ID: strconv.FormatInt(id, 10), Products: []categorize.Product{}}
	err := d.sql.QueryRowContext(ctx, `SELECT COALESCE(name,''),COALESCE(inn,''),source,url FROM company WHERE id=?`, id).Scan(&c.Name, &c.INN, &c.Source, &c.URL)
	if err != nil {
		return c, err
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT id,COALESCE(items_json,''),COALESCE(file_path,''),COALESCE(kind,'') FROM price_list WHERE company_id=? AND is_active=1 ORDER BY id`, id)
	if err != nil {
		return c, err
	}
	type price struct {
		id              int64
		raw, path, kind string
	}
	var prices []price
	for rows.Next() {
		var p price
		if err := rows.Scan(&p.id, &p.raw, &p.path, &p.kind); err != nil {
			rows.Close()
			return c, err
		}
		prices = append(prices, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return c, err
	}
	seen := map[string]bool{}
	for _, p := range prices {
		if err := ctx.Err(); err != nil {
			return c, err
		}
		if p.raw != "" {
			var items []models.PriceItem
			if err := json.Unmarshal([]byte(p.raw), &items); err != nil {
				return c, fmt.Errorf("company_id=%d: items_json: %w", id, err)
			}
			for _, item := range items {
				appendUnique(&c, []categorize.Product{{Name: item.Name, Description: item.Desc, SourceCategory: item.Category, URL: item.URL}}, seen)
			}
		}
		if p.kind == "file" {
			if p.path == "" {
				c.Warnings = append(c.Warnings, fmt.Sprintf("Прайс %d: файл ещё не скачан", p.id))
				continue
			}
			result, err := d.fileProducts(ctx, p.id, p.path)
			if ctx.Err() != nil {
				return c, ctx.Err()
			}
			if err != nil {
				warning := fmt.Sprintf("Прайс %d (%s): %v", p.id, p.path, err)
				c.Warnings = append(c.Warnings, warning)
				log.Printf("[pricefiles] %s", warning)
				continue
			}
			appendUnique(&c, result.Products, seen)
			if result.Status != "extracted" {
				c.Warnings = append(c.Warnings, fmt.Sprintf("Прайс %d: %s (%s)", p.id, result.Message, result.Status))
			}
		}
	}
	return c, nil
}

func (d *DB) CategorizeCompany(ctx context.Context, id int64, m *categorize.Matcher) (categorize.CompanyResult, error) {
	c, err := d.CategoryCompany(ctx, id)
	if err != nil {
		return categorize.CompanyResult{}, err
	}
	r := m.Classify(c)
	names := []string{}
	for _, a := range r.Categories {
		names = append(names, a.Category.Name)
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return r, err
	}
	_, err = d.sql.ExecContext(ctx, `UPDATE company SET category_normal_json=? WHERE id=?`, string(raw), id)
	return r, err
}
func (d *DB) CheckCategorySchema(ctx context.Context) error {
	rows, err := d.sql.QueryContext(ctx, `SELECT category_normal_json FROM company LIMIT 0`)
	if err != nil {
		return fmt.Errorf("нужна миграция 000005: выполните make migrate-up: %w", err)
	}
	rows.Close()
	rows, err = d.sql.QueryContext(ctx, `SELECT price_list_id,fingerprint,parser_version,products_json,status,message FROM price_file_extraction LIMIT 0`)
	if err != nil {
		return fmt.Errorf("нужна миграция 000006: выполните make migrate-up: %w", err)
	}
	return rows.Close()
}
