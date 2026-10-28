package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"suppliers-parser/internal/core"
	"suppliers-parser/internal/runner"
	"suppliers-parser/internal/storage"

	_ "suppliers-parser/internal/sources/productcenter"
	_ "suppliers-parser/internal/sources/yopt"
)

func main() {
	sourcesFlag := flag.String("sources", "yopt", "источники через запятую или 'all'")
	dbPath := flag.String("db", "data.db", "путь к SQLite")
	force := flag.Bool("force", false, "обновить существующие компании и прайсы с сайта, добавить новые")
	downloadsOnly := flag.Bool("downloads-only", false, "скачать недостающие файлы из БД без парсинга сайтов")
	flag.Parse()
	if *force && *downloadsOnly {
		log.Fatal("--force и --downloads-only несовместимы: выберите обновление с сайта или дозагрузку из БД")
	}
	if err := storage.CleanupPriceFiles(); err != nil {
		log.Fatalf("очистка файлов: %v", err)
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("db: %v", err)
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
				log.Fatal(err)
			}
			sources = append(sources, src)
		}
	}

	r := runner.New(db).WithForce(*force).WithDownloadsOnly(*downloadsOnly)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	results := r.RunSources(ctx, sources)

	fmt.Println("\n=== Итог ===")
	for name, s := range results {
		fmt.Printf("[%s] компаний: %d, прайсов: %d, пропущено: %d, ошибок: %d\n", name, s.SavedCompanies, s.SavedPrices, s.Skipped, s.Failed)
	}
}
