package categorize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Report struct {
	Input         string          `json:"input"`
	Taxonomy      string          `json:"taxonomy"`
	CategoryCount int             `json:"category_count"`
	Method        string          `json:"method"`
	Notes         []string        `json:"notes"`
	Summary       Summary         `json:"summary"`
	Companies     []CompanyResult `json:"companies"`
}

type Summary struct {
	Companies               int `json:"companies"`
	CompaniesWithProducts   int `json:"companies_with_products"`
	CompaniesWithCategories int `json:"companies_with_categories"`
	Products                int `json:"products"`
	MatchedProducts         int `json:"matched_products"`
}

func summarize(companies []CompanyResult) Summary {
	s := Summary{Companies: len(companies)}
	for _, c := range companies {
		if len(c.Products) > 0 {
			s.CompaniesWithProducts++
		}
		if len(c.Categories) > 0 {
			s.CompaniesWithCategories++
		}
		s.Products += len(c.Products)
		for _, p := range c.Products {
			if len(p.Matches) > 0 {
				s.MatchedProducts++
			}
		}
	}
	return s
}

func label(status string) string {
	switch status {
	case "matched":
		return "Подобраны"
	case "partial":
		return "Подобраны частично"
	case "insufficient_data":
		return "Недостаточно данных"
	default:
		return "Нет соответствий"
	}
}
func escape(s string) string {
	return strings.NewReplacer("|", "\\|", "\n", " ", "\r", " ", "`", "'").Replace(s)
}
func WriteReport(dir string, r Report) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	r.Summary = summarize(r.Companies)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), append(data, '\n'), 0644); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprint(&b, "# Подбор категорий компаний\n\n")
	fmt.Fprintf(&b, "Источник: `%s`. Справочник: `%s`, уникальных названий: %d.\n\n", escape(r.Input), escape(r.Taxonomy), r.CategoryCount)
	fmt.Fprintf(&b, "Компаний: **%d**, с товарами: **%d**, с подобранными категориями: **%d**. Товаров: **%d**, с соответствиями: **%d**. Это покрытие правил, а не оценка точности.\n\n", r.Summary.Companies, r.Summary.CompaniesWithProducts, r.Summary.CompaniesWithCategories, r.Summary.Products, r.Summary.MatchedProducts)
	for _, note := range r.Notes {
		fmt.Fprintf(&b, "- %s\n", note)
	}
	for _, c := range r.Companies {
		fmt.Fprintf(&b, "\n## %s (ID %s)\n\nСтатус: **%s**. Товаров: %d.\n", escape(c.Name), escape(c.ID), label(c.Status), len(c.Products))
		for _, w := range c.Warnings {
			fmt.Fprintf(&b, "\n%s\n", escape(w))
		}
		if len(c.Categories) > 0 {
			fmt.Fprint(&b, "\nКатегории компании:\n\n")
			for _, a := range c.Categories {
				fmt.Fprintf(&b, "- **%s** — строка справочника %d, товаров-оснований: %d.\n", escape(a.Category.Name), a.Category.ID, len(a.ProductRows))
			}
		}
		if len(c.Products) == 0 {
			fmt.Fprintln(&b, "\nНет однозначно извлечённых товарных строк. Категория компании и название файла прайса не используются как доказательство.")
			continue
		}
		fmt.Fprintln(&b, "\n| Строка | Товар | Подобранные категории | Основание | Источник |\n|---|---|---|---|---|")
		for _, p := range c.Products {
			var names, evidence []string
			for _, m := range p.Matches {
				names = append(names, m.Category.Name)
				evidence = append(evidence, m.Evidence)
			}
			if len(names) == 0 {
				names = []string{"Не подобраны"}
				evidence = []string{"Нужно проверить вручную или расширить словарь"}
			}
			source := p.Product.URL
			if p.Product.SourceFile != "" {
				source = fmt.Sprintf("%s; лист=%s; страница=%d; строка=%d", p.Product.SourceFile, p.Product.SourceSheet, p.Product.SourcePage, p.Product.SourceRow)
			}
			fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n", p.Product.Row, escape(p.Product.Name), escape(strings.Join(names, "; ")), escape(strings.Join(evidence, "; ")), escape(source))
		}
	}
	return os.WriteFile(filepath.Join(dir, "report.md"), []byte(b.String()), 0644)
}
