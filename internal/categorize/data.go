package categorize

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/xuri/excelize/v2"
)

type Category struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Lines []int  `json:"source_lines"`
}

type Product struct {
	Row            int    `json:"excel_row"`
	SourceFile     string `json:"source_file,omitempty"`
	SourceSheet    string `json:"source_sheet,omitempty"`
	SourcePage     int    `json:"source_page,omitempty"`
	SourceRow      int    `json:"source_row,omitempty"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	SourceCategory string `json:"source_category,omitempty"`
	URL            string `json:"url,omitempty"`
}

type Company struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	INN      string    `json:"inn,omitempty"`
	Source   string    `json:"source,omitempty"`
	URL      string    `json:"url,omitempty"`
	Products []Product `json:"products"`
	Warnings []string  `json:"warnings,omitempty"`
}

// CompanySource keeps the classification independent of Excel or SQLite.
type CompanySource interface {
	Load(context.Context, int) ([]Company, error)
}

type ExcelSource struct{ Path string }

func LoadCategories(path string) ([]Category, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 1024*1024)
	var result []Category
	seen := map[string]int{}
	line := 0
	for scan.Scan() {
		line++
		name := strings.TrimSpace(strings.TrimPrefix(scan.Text(), "\ufeff"))
		if name == "" {
			continue
		}
		key := normalize(name)
		if i, ok := seen[key]; ok {
			result[i].Lines = append(result[i].Lines, line)
			continue
		}
		seen[key] = len(result)
		result = append(result, Category{ID: line, Name: name, Lines: []int{line}})
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("справочник категорий пуст")
	}
	return result, nil
}

func table(f *excelize.File, sheet string, required ...string) ([][]string, map[string]int, error) {
	rows, err := f.GetRows(sheet)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, fmt.Errorf("лист %q пуст", sheet)
	}
	header := map[string]int{}
	for i, name := range rows[0] {
		name = strings.TrimSpace(name)
		if _, ok := header[name]; ok && name != "" {
			return nil, nil, fmt.Errorf("лист %s: повторяющийся заголовок %s", sheet, name)
		}
		header[name] = i
	}
	for _, name := range required {
		if _, ok := header[name]; !ok {
			return nil, nil, fmt.Errorf("лист %s: нет столбца %s", sheet, name)
		}
	}
	return rows[1:], header, nil
}
func cell(row []string, header map[string]int, name string) string {
	i, ok := header[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}
func companyKey(name, inn string) string { return normalize(name) + "\x00" + strings.TrimSpace(inn) }

func (s ExcelSource) Load(ctx context.Context, limit int) ([]Company, error) {
	if limit < 1 {
		return nil, fmt.Errorf("limit должен быть положительным")
	}
	f, err := excelize.OpenFile(s.Path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, h, err := table(f, "Компании", "Название", "ИНН")
	if err != nil {
		return nil, err
	}
	allKeys := map[string]int{}
	for _, r := range rows {
		if cell(r, h, "Название") != "" {
			allKeys[companyKey(cell(r, h, "Название"), cell(r, h, "ИНН"))]++
		}
	}
	var companies []Company
	selected := map[string]int{}
	for _, r := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := cell(r, h, "Название")
		if name == "" {
			continue
		}
		c := Company{ID: cell(r, h, "ID"), Name: name, INN: cell(r, h, "ИНН"), Source: cell(r, h, "Источник"), URL: cell(r, h, "Ссылка"), Products: []Product{}}
		key := companyKey(name, c.INN)
		if allKeys[key] > 1 {
			c.Warnings = append(c.Warnings, "Неоднозначная связь по названию и ИНН: нужны ID компаний в листе Товары")
		} else {
			selected[key] = len(companies)
		}
		companies = append(companies, c)
		if len(companies) == limit {
			break
		}
	}
	products, ph, err := table(f, "Товары", "Компания", "ИНН", "Название товара")
	if err != nil {
		return nil, err
	}
	for i, r := range products {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		index, ok := selected[companyKey(cell(r, ph, "Компания"), cell(r, ph, "ИНН"))]
		if !ok {
			continue
		}
		name := cell(r, ph, "Название товара")
		if name == "" {
			continue
		}
		companies[index].Products = append(companies[index].Products, Product{Row: i + 2, Name: name, Description: cell(r, ph, "Описание"), SourceCategory: cell(r, ph, "Категория товара"), URL: cell(r, ph, "Ссылка на товар")})
	}
	return companies, nil
}
