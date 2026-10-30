package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"suppliers-parser/internal/categorize"
	"suppliers-parser/internal/core"
	"suppliers-parser/internal/enrichment"
	"suppliers-parser/internal/leadership"
	"suppliers-parser/internal/runner"
	"suppliers-parser/internal/storage"

	_ "suppliers-parser/internal/sources/productcenter"
	_ "suppliers-parser/internal/sources/suplbiz"
	_ "suppliers-parser/internal/sources/yopt"
)

func main() {
	sourcesFlag := flag.String("sources", "yopt", "источники через запятую или 'all'")
	dbPath := flag.String("db", "data.db", "путь к SQLite")
	force := flag.Bool("force", false, "обновить существующие компании и прайсы с сайта, добавить новые")
	downloadsOnly := flag.Bool("downloads-only", false, "скачать недостающие файлы из БД без парсинга сайтов")
	taxonomy := flag.String("categories", "category-normal", "справочник нормальных категорий")
	aliases := flag.String("aliases", "config/category-aliases.json", "словарь категорий")
	flag.Parse()
	if *force && *downloadsOnly {
		log.Fatal("[parse] --force и --downloads-only несовместимы: выберите обновление с сайта или дозагрузку из БД")
	}
	if err := storage.CleanupPriceFiles(); err != nil {
		log.Fatalf("[parse] очистка файлов: %v", err)
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("[parse] db: %v", err)
	}
	defer db.Close()

	var sources []core.Source
	if *sourcesFlag == "all" {
		sources = core.All()
	} else {
		for _, name := range strings.Split(*sourcesFlag, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			src, err := core.Get(name)
			if err != nil {
				log.Fatalf("[parse] ошибка: %v", err)
			}
			sources = append(sources, src)
		}
	}

	r := runner.New(db).WithForce(*force).WithDownloadsOnly(*downloadsOnly)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	{
		categories, err := categorize.LoadCategories(*taxonomy)
		if err != nil {
			log.Fatalf("[parse] ошибка: %v", err)
		}
		var rules []categorize.Alias
		if *aliases != "" {
			rules, err = categorize.LoadAliases(*aliases)
			if err != nil {
				log.Fatalf("[parse] ошибка: %v", err)
			}
		}
		matcher, err := categorize.NewMatcher(categories, rules)
		if err != nil {
			log.Fatalf("[parse] ошибка: %v", err)
		}
		if err := db.CheckCategorySchema(ctx); err != nil {
			log.Fatalf("[parse] ошибка: %v", err)
		}
		if err := db.CheckLeadershipSchema(ctx); !*downloadsOnly && err != nil {
			log.Fatalf("[parse] ошибка: %v", err)
		}
		service := &enrichment.Service{DB: db, Matcher: matcher}
		if key := strings.TrimSpace(os.Getenv("DADATA_API_KEY")); !*downloadsOnly && key != "" {
			service.Provider, err = leadership.NewDaData(key, 20*time.Second)
			if err != nil {
				log.Fatalf("[parse] ошибка: %v", err)
			}
		} else if !*downloadsOnly {
			log.Printf("[enrich] поиск руководителя отключён: DADATA_API_KEY не задан")
		}
		r.WithEnrichment(service.Process).WithFileEnrichment(service.ProcessFiles)
	}
	results := r.RunSources(ctx, sources)

	log.Print("[parse] итог")
	for name, s := range results {
		log.Printf("[%s] компаний: %d, прайсов: %d, пропущено: %d, ошибок: %d", name, s.SavedCompanies, s.SavedPrices, s.Skipped, s.Failed)
	}
}
