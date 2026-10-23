package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"suppliers-parser/internal/core"
	"suppliers-parser/internal/runner"
	"suppliers-parser/internal/storage"

	_ "suppliers-parser/internal/sources/yopt"
)

func main() {
	sourcesFlag := flag.String("sources", "yopt", "источники через запятую или 'all'")
	dbPath := flag.String("db", "data.db", "путь к SQLite")
	force := flag.Bool("force", false, "перепарсить все")
	flag.Parse()

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

	r := runner.New(db).WithForce(*force)
	results := r.RunSources(context.Background(), sources)

	fmt.Println("\n=== Итог ===")
	for name, s := range results {
		fmt.Printf("[%s] компаний: %d, прайсов: %d, пропущено: %d, ошибок: %d\n", name, s.SavedCompanies, s.SavedPrices, s.Skipped, s.Failed)
	}
}
