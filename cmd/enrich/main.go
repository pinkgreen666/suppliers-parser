package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"suppliers-parser/internal/leadership"
	"suppliers-parser/internal/storage"
)

func run() error {
	dbPath := flag.String("db", "data.db", "путь к SQLite")
	sourcesFlag := flag.String("sources", "all", "all или yopt,productcenter")
	limit := flag.Int("limit", 0, "число компаний; 0 — все активные")
	force := flag.Bool("force", false, "обновить сведения независимо от кеша")
	dryRun := flag.Bool("dry-run", false, "проверить реквизиты без API и изменений БД")
	ttl := flag.Duration("ttl", 30*24*time.Hour, "срок кеша успешных ответов")
	retry := flag.Duration("retry-after", time.Hour, "пауза перед повтором ошибки при следующем запуске")
	delay := flag.Duration("delay", time.Second, "пауза между запросами")
	timeout := flag.Duration("timeout", 20*time.Second, "таймаут HTTP-запроса")
	out := flag.String("out", "outputs/leadership/report.json", "JSON-отчёт")
	flag.Parse()
	if *limit < 0 || *ttl <= 0 || *retry <= 0 || *delay < 0 {
		return fmt.Errorf("некорректные параметры limit/ttl/retry-after/delay")
	}
	var sources []string
	if *sourcesFlag != "all" {
		for _, s := range strings.Split(*sourcesFlag, ",") {
			s = strings.TrimSpace(s)
			if s != "yopt" && s != "productcenter" {
				return fmt.Errorf("неизвестный источник %q", s)
			}
			sources = append(sources, s)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Opening a nonexistent SQLite path would create an empty file by mistake.
	if _, err := os.Stat(*dbPath); err != nil {
		return err
	}
	db, err := storage.Open(*dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	companies, err := db.LeadershipCompanies(ctx, sources, *limit)
	if err != nil {
		return err
	}
	var provider leadership.Provider
	if !*dryRun {
		if err := db.CheckLeadershipSchema(ctx); err != nil {
			return err
		}
		provider, err = leadership.NewDaData(os.Getenv("DADATA_API_KEY"), *timeout)
		if err != nil {
			return err
		}
	}
	report, runErr := leadership.Run(ctx, db, provider, companies, leadership.Options{Force: *force, DryRun: *dryRun, TTL: *ttl, RetryAfter: *retry, Delay: *delay})
	if runErr != nil {
		report.Error = runErr.Error()
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0600); err != nil {
		return err
	}
	log.Printf("[enrich] выбрано=%d, обработано=%d, запросов=%d, отчёт=%s", report.Selected, len(report.Records), report.Requests, *out)
	return runErr
}
func main() {
	if err := run(); err != nil {
		log.Print(err)
		os.Exit(1)
	}
}
