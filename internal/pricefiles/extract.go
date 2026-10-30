// Package pricefiles extracts explicit product tables, never OCR or arbitrary prose.
package pricefiles

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
	"suppliers-parser/internal/categorize"
)

const Version = 2
const MaxFileSize = 64 << 20
const maxRows = 100000

type Result struct {
	Products []categorize.Product `json:"products"`
	Status   string               `json:"status"`
	Message  string               `json:"message,omitempty"`
}

func Extract(ctx context.Context, path string) (Result, error) {
	result := Result{Products: []categorize.Product{}, Status: "no_table"}
	info, err := os.Stat(path)
	if err != nil {
		return result, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileSize {
		return result, fmt.Errorf("необычный файл или размер больше 64 MiB")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xlsx", ".xlsm":
		result.Products, err = workbook(ctx, path)
	case ".xls":
		// Isolated profile prevents interference with an open LibreOffice session.
		dir, e := os.MkdirTemp("", "supplier-xls-*")
		if e != nil {
			return result, e
		}
		defer os.RemoveAll(dir)
		abs, e := filepath.Abs(path)
		if e != nil {
			return result, e
		}
		profile := (&url.URL{Scheme: "file", Path: filepath.Join(dir, "profile")}).String()
		_, err = command(ctx, "libreoffice", "-env:UserInstallation="+profile, "--headless", "--convert-to", "xlsx", "--outdir", dir, abs)
		if err == nil {
			result.Products, err = workbook(ctx, filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+".xlsx"))
		}
	case ".csv", ".tsv":
		result.Products, err = csvFile(ctx, path)
	case ".pdf":
		var text []byte
		abs, e := filepath.Abs(path)
		if e != nil {
			return result, e
		}
		text, err = command(ctx, "pdftotext", "-layout", "-enc", "UTF-8", abs, "-")
		if err == nil {
			if strings.TrimSpace(string(text)) == "" {
				result.Status = "no_text"
				result.Message = "нет текстового слоя; OCR отключён"
				return result, nil
			}
			result.Products = pdfTables(string(text))
		}
	default:
		result.Status = "unsupported"
		result.Message = "формат файла не поддерживается"
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	for i := range result.Products {
		result.Products[i].SourceFile = path
	}
	if len(result.Products) > 0 {
		result.Status = "extracted"
	} else {
		result.Message = "таблица с явным столбцом наименования не найдена"
	}
	return result, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 16<<20 {
		return 0, fmt.Errorf("вывод превышает 16 MiB")
	}
	return b.Buffer.Write(p)
}
func command(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	var out, stderr boundedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return out.Bytes(), nil
}
func clean(s string) string {
	return strings.Join(strings.Fields(strings.TrimPrefix(s, "\ufeff")), " ")
}
func nameHeader(s string) bool {
	s = strings.ToLower(clean(s))
	s = strings.Trim(s, " .:")
	switch s {
	case "наименование", "наименование товара", "наименование продукции", "наименование изделия", "наименование товара/услуги", "название", "название товара", "номенклатура", "товар", "товары", "product", "product name", "item name", "description":
		return true
	}
	return false
}
func validName(s string) bool {
	s = clean(s)
	if len([]rune(s)) < 3 || len([]rune(s)) > 500 || nameHeader(s) {
		return false
	}
	lower := strings.ToLower(s)
	for _, prefix := range []string{"итого", "всего", "примечание", "доставка", "условия оплаты", "наименование организации", "телефон", "http://", "https://"} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	letters := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return letters >= 3
}
func tableRows(ctx context.Context, rows [][]string, sheet string) ([]categorize.Product, error) {
	var products []categorize.Product
	col := -1
	columns := 0
	for i, row := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if i >= maxRows {
			return nil, fmt.Errorf("лист превышает %d строк", maxRows)
		}
		header := -1
		for j, cell := range row {
			if nameHeader(cell) {
				header = j
				break
			}
		}
		if header >= 0 {
			col = header
			columns = len(row)
			continue
		}
		if col < 0 || col >= len(row) {
			continue
		}
		name := clean(row[col])
		if !validName(name) {
			continue
		}
		other := false
		for j, v := range row {
			if j != col && clean(v) != "" {
				other = true
			}
		}
		if columns > 1 && !other {
			continue
		} // section titles are not products
		products = append(products, categorize.Product{Name: name, SourceSheet: sheet, SourceRow: i + 1})
	}
	return products, nil
}
func workbook(ctx context.Context, path string) ([]categorize.Product, error) {
	f, err := excelize.OpenFile(path, excelize.Options{UnzipSizeLimit: 128 << 20, UnzipXMLSizeLimit: 16 << 20})
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var products []categorize.Product
	for _, sheet := range f.GetSheetList() {
		rows, err := f.Rows(sheet)
		if err != nil {
			return nil, err
		}
		var data [][]string
		for rows.Next() {
			if err := ctx.Err(); err != nil {
				rows.Close()
				return nil, err
			}
			if len(data) >= maxRows {
				rows.Close()
				return nil, fmt.Errorf("лист превышает %d строк", maxRows)
			}
			row, err := rows.Columns()
			if err != nil {
				rows.Close()
				return nil, err
			}
			data = append(data, row)
		}
		err = rows.Error()
		rows.Close()
		if err != nil {
			return nil, err
		}
		items, err := tableRows(ctx, data, sheet)
		if err != nil {
			return nil, err
		}
		products = append(products, items...)
	}
	return products, nil
}
func csvFile(ctx context.Context, path string) ([]categorize.Product, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(b) {
		b, err = charmap.Windows1251.NewDecoder().Bytes(b)
		if err != nil {
			return nil, err
		}
	}
	text := strings.TrimPrefix(string(b), "\ufeff")
	// Choose the delimiter giving the widest actual header; quoted separators are ignored.
	var best [][]string
	bestWidth := 0
	for _, sep := range []rune{';', '\t', ','} {
		r := csv.NewReader(strings.NewReader(text))
		r.Comma = sep
		r.FieldsPerRecord = -1
		var rows [][]string
		width := 0
		failed := false
		for {
			row, e := r.Read()
			if e == io.EOF {
				break
			}
			if e != nil {
				failed = true
				break
			}
			if len(rows) >= maxRows {
				return nil, fmt.Errorf("CSV превышает лимит строк")
			}
			rows = append(rows, row)
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			for _, cell := range row {
				if nameHeader(cell) && len(row) > width {
					width = len(row)
				}
			}
		}
		if !failed && width > bestWidth {
			best = rows
			bestWidth = width
		}
	}
	return tableRows(ctx, best, "")
}

var gaps = regexp.MustCompile(`\S(?:.*?\S)?(?: {2,}|$)`)
var number = regexp.MustCompile(`\d`)

// PDF columns retain physical positions from pdftotext -layout, including wrapped names.
func pdfTables(text string) []categorize.Product {
	var products []categorize.Product
	for page, body := range strings.Split(text, "\f") {
		start, end := -1, -1
		var parts []string
		hasValue := false
		lineNumber := 0
		flush := func() {
			name := clean(strings.Join(parts, " "))
			if hasValue && validName(name) {
				products = append(products, categorize.Product{Name: name, SourcePage: page + 1, SourceRow: lineNumber})
			}
			parts = nil
			hasValue = false
		}
		for lineIndex, line := range strings.Split(body, "\n") {
			// Find header cells by runs of two or more spaces, in rune coordinates.
			headerFound := false
			previousEnd := 0
			for _, loc := range gaps.FindAllStringIndex(line, -1) {
				cell := strings.TrimSpace(line[loc[0]:loc[1]])
				if nameHeader(cell) {
					flush()
					start = previousEnd
					if start > 0 {
						start += 2
					}
					end = utf8.RuneCountInString(line[:loc[1]])
					if end >= utf8.RuneCountInString(line) {
						end = -1
					}
					headerFound = true
					break
				}
				previousEnd = utf8.RuneCountInString(strings.TrimRight(line[:loc[1]], " "))
			}
			if headerFound {
				continue
			}
			if start < 0 {
				continue
			}
			if strings.TrimSpace(line) == "" {
				flush()
				continue
			}
			runes := []rune(line)
			a := start
			if a > 0 {
				a--
			}
			b := end
			if b < 0 || b > len(runes) {
				b = len(runes)
			}
			if a >= len(runes) || b <= a {
				continue
			}
			name := clean(string(runes[a:b]))
			outside := string(runes[b:])
			if end < 0 {
				outside = string(runes[:a])
			}
			numeric := number.MatchString(outside)
			if numeric && hasValue {
				flush()
			}
			if len(parts) == 0 {
				lineNumber = lineIndex + 1
			}
			if name != "" {
				parts = append(parts, name)
			}
			hasValue = hasValue || numeric
		}
		flush()
	}
	return products
}
