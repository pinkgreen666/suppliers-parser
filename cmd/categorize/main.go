package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"

	"suppliers-parser/internal/categorize"
)

func main() {
	input := flag.String("input", "companies.xlsx", "Excel с листами Компании и Товары")
	taxonomy := flag.String("categories", "category-normal", "справочник: одна конечная категория на строку")
	aliases := flag.String("aliases", "config/category-aliases.json", "JSON со словарём синонимов; пустая строка отключает словарь")
	limit := flag.Int("limit", 5, "число компаний в порядке листа Компании")
	out := flag.String("out", "outputs/category-preview", "каталог для report.json и report.md")
	flag.Parse()
	if *limit < 1 {
		log.Fatal("--limit должен быть больше нуля")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	categories, err := categorize.LoadCategories(*taxonomy)
	if err != nil {
		log.Fatal(err)
	}
	var rules []categorize.Alias
	if *aliases != "" {
		rules, err = categorize.LoadAliases(*aliases)
		if err != nil {
			log.Fatal(err)
		}
	}
	matcher, err := categorize.NewMatcher(categories, rules)
	if err != nil {
		log.Fatal(err)
	}
	var source categorize.CompanySource = categorize.ExcelSource{Path: *input}
	companies, err := source.Load(ctx, *limit)
	if err != nil {
		log.Fatal(err)
	}
	if len(companies) == 0 {
		log.Fatal("в Excel нет компаний")
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
			log.Fatal(err)
		}
		r := matcher.Classify(c)
		report.Companies = append(report.Companies, r)
		log.Printf("[%d/%d] %s: товаров=%d, категорий=%d, статус=%s", i+1, len(companies), c.Name, len(c.Products), len(r.Categories), r.Status)
	}
	if err := categorize.WriteReport(*out, report); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Результат: %s/report.md и %s/report.json\n", *out, *out)
}
