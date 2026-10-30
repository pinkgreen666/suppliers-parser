package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"suppliers-parser/internal/categorize"
	"suppliers-parser/internal/pricefiles"
)

func (d *DB) fileProducts(ctx context.Context, id int64, path string) (pricefiles.Result, error) {
	var result pricefiles.Result
	f, err := os.Open(path)
	if err != nil {
		return result, err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, pricefiles.MaxFileSize+1))
	f.Close()
	if err != nil {
		return result, err
	}
	if n > pricefiles.MaxFileSize {
		return result, fmt.Errorf("файл превышает 64 MiB")
	}
	fingerprint := hex.EncodeToString(h.Sum(nil))
	var raw string
	err = d.sql.QueryRowContext(ctx, `SELECT products_json,status,message FROM price_file_extraction WHERE price_list_id=? AND fingerprint=? AND parser_version=?`, id, fingerprint, pricefiles.Version).Scan(&raw, &result.Status, &result.Message)
	if err == nil {
		if err := json.Unmarshal([]byte(raw), &result.Products); err != nil {
			return result, err
		}
		for i := range result.Products {
			result.Products[i].SourceFile = path
		}
		return result, nil
	}
	if err != sql.ErrNoRows {
		return result, err
	}
	log.Printf("[pricefiles] начало: price_list_id=%d, файл=%s", id, path)
	result, err = pricefiles.Extract(ctx, path)
	if err != nil {
		return result, err
	}
	rawBytes, err := json.Marshal(result.Products)
	if err != nil {
		return result, err
	}
	_, err = d.sql.ExecContext(ctx, `INSERT INTO price_file_extraction(price_list_id,fingerprint,parser_version,products_json,status,message) VALUES(?,?,?,?,?,?) ON CONFLICT(price_list_id) DO UPDATE SET fingerprint=excluded.fingerprint,parser_version=excluded.parser_version,products_json=excluded.products_json,status=excluded.status,message=excluded.message`, id, fingerprint, pricefiles.Version, string(rawBytes), result.Status, result.Message)
	log.Printf("[pricefiles] готово: price_list_id=%d, товаров=%d, статус=%s, %s", id, len(result.Products), result.Status, result.Message)
	return result, err
}
func appendUnique(c *categorize.Company, products []categorize.Product, seen map[string]bool) {
	for _, p := range products {
		key := strings.ToLower(strings.Join(strings.Fields(p.Name), " "))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		p.Row = len(c.Products) + 1
		c.Products = append(c.Products, p)
	}
}
