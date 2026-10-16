package main

import (
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"suppliers-parser/internal/core"
	"suppliers-parser/internal/storage"
	"time"
)

func main() {
	var (
		sourceName = flag.String("source", "yopt", "источник (yopt, ...)")
		dbPath     = flag.String("db", "data.db", "путь к SQLite")
		workers    = flag.Int("workers", 5, "параллельных воркеров")
		batchSize  = flag.Int("batch", 10, "карточек в батче")
		delayMs    = flag.Int("delay", 300, "пауза между запросами мс")
		maxPages   = flag.Int("max-pages", 0, "максимум страниц (0 = все)")
		force      = flag.Bool("force", false, "перепарсить все")
	)
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if *sourceName == "" {
		fmt.Println("Доступные источники:", core.List())
		return
	}

	db, err := storage.Open(*dbPath)
	if err != nil {
		log.Fatalf("открытие БД: %v", err)
	}
	defer db.Close()

	src, err := core.Get(*sourceName)
	if err != nil {
		log.Fatalf("источник: %v", err)
	}

	runner := &core.Runner{
		DB:        db,
		Source:    src,
		Workers:   *workers,
		BatchSize: *batchSize,
		Delay:     time.Duration(*delayMs) * time.Millisecond,
		MaxPages:  *maxPages,
		Force:     *force,
	}

	if err := runner.Run(); err != nil {
		log.Fatalf("run: %v", err)
	}
	slog.Info("готово", "source", *sourceName)
}
