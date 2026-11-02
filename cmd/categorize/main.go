package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"suppliers-parser/internal/storage"

	"suppliers-parser/internal/categorize"
)

func main() {
	dbPath := flag.String("db", "", "SQLite: читать товары и сохранять категории вместо Excel")
	input := flag.String("input", "companies.xlsx", "Excel с листами Компании и Товары")
	taxonomy := flag.String("categories", "category-normal", "справочник: одна конечная категория на строку")
	aliases := flag.String("aliases", "config/category-aliases.json", "JSON со словарём синонимов; пустая строка отключает словарь")
	limit := flag.Int("limit", 5, "число компаний (SQLite: 0 = все)")
	out := flag.String("out", "outputs/category-preview", "каталог для report.json и report.md")
	flag.Parse()
	if *limit < 0 || (*dbPath == "" && *limit == 0) {
		log.Fatal("[categorize] --limit должен быть положительным для Excel, неотрицательным для SQLite")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	categories, err := categorize.LoadCategories(*taxonomy)
	if err != nil {
		log.Fatalf("[categorize] ошибка: %v", err)
	}
	var rules []categorize.Alias
	if *aliases != "" {
		rules, err = categorize.LoadAliases(*aliases)
		if err != nil {
			log.Fatalf("[categorize] ошибка: %v", err)
		}
	}
	matcher, err := categorize.NewMatcher(categories, rules)
	if err != nil {
		log.Fatalf("[categorize] ошибка: %v", err)
	}
	if *dbPath != "" {
		db, err := storage.Open(*dbPath)
		if err != nil {
			log.Fatalf("[categorize] ошибка: %v", err)
		}
		defer db.Close()
		if err := db.CheckCategorySchema(ctx); err != nil {
			log.Fatalf("[categorize] ошибка: %v", err)
		}
		companies, err := db.LeadershipCompanies(ctx, nil, *limit)
		if err != nil {
			log.Fatalf("[categorize] ошибка: %v", err)
		}
		report := categorize.Report{Input: *dbPath, Taxonomy: *taxonomy, CategoryCount: len(categories), Method: "local_rules_v2", Companies: []categorize.CompanyResult{}}
		for i, c := range companies {
			r, err := db.CategorizeCompany(ctx, c.ID, matcher)
			if err != nil {
				log.Fatalf("[categorize] ошибка: %v", err)
			}
			report.Companies = append(report.Companies, r)
			log.Printf("[categorize] [%d/%d] company_id=%d: категорий=%d, статус=%s", i+1, len(companies), c.ID, len(r.Categories), r.Status)
		}
		if err := categorize.WriteReport(*out, report); err != nil {
			log.Fatalf("[categorize] ошибка: %v", err)
		}
		log.Printf("[categorize] готово: компаний=%d, отчёт=%s", len(companies), *out)
		return
	}
	var source categorize.CompanySource = categorize.ExcelSource{Path: *input}
	companies, err := source.Load(ctx, *limit)
	if err != nil {
		log.Fatalf("[categorize] ошибка: %v", err)
	}
	if len(companies) == 0 {
		log.Fatal("[categorize] в Excel нет компаний")
	}
	report := categorize.Report{Input: *input, Taxonomy: *taxonomy, CategoryCount: len(categories), Method: "local_rules_v2", Notes: []string{
		"Локальные правила и синонимы, без LLM и внешних API. Результат предварительный и требует проверки.",
		"Для части категорий есть только совпадение по названию. Неоднозначные категории без контекста отключены в словаре.",
		"Подбор основан на названии товара. Общая категория компании и упоминания в описаниях не являются доказательством продажи товара.",
		"Отсутствие соответствия может означать недостаточность словаря. Программа не назначает ближайшую категорию принудительно.",
		"Повторяющиеся названия справочника объединены; ID — номер первого вхождения. Для различения одинаковых названий нужны полные пути категорий.",
		"Файлы прайс-листов не разбираются. Компания без товарных строк получает статус «Недостаточно данных».",
	}, Companies: []categorize.CompanyResult{}}
	for i, c := range companies {
		if err := ctx.Err(); err != nil {
			log.Fatalf("[categorize] ошибка: %v", err)
		}
		r := matcher.Classify(c)
		report.Companies = append(report.Companies, r)
		log.Printf("[categorize] [%d/%d] %s: товаров=%d, категорий=%d, статус=%s", i+1, len(companies), c.Name, len(c.Products), len(r.Categories), r.Status)
	}
	if err := categorize.WriteReport(*out, report); err != nil {
		log.Fatalf("[categorize] ошибка: %v", err)
	}
	log.Printf("[categorize] результат: %s/report.md и %s/report.json\n", *out, *out)
}
