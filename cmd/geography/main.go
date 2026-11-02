package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"suppliers-parser/internal/storage"
)

func main() {
	path := flag.String("db", "data.db", "существующая SQLite-база")
	flag.Parse()
	if _, err := os.Stat(*path); err != nil {
		log.Fatalf("[geography] %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := storage.Open(*path)
	if err != nil {
		log.Fatalf("[geography] %v", err)
	}
	defer db.Close()
	records, err := db.ClassifyGeography(ctx)
	if err != nil {
		log.Fatalf("[geography] нужна миграция 000007 и корректные данные: %v", err)
	}
	dir := filepath.Join(filepath.Dir(*path), "outputs", "geography")
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Fatalf("[geography] %v", err)
	}
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		log.Fatalf("[geography] %v", err)
	}
	report := filepath.Join(dir, "report.json")
	if err := os.WriteFile(report, raw, 0600); err != nil {
		log.Fatalf("[geography] %v", err)
	}
	yes, no, unknown := 0, 0, 0
	for _, r := range records {
		if r.IsRussian == nil {
			unknown++
		} else if *r.IsRussian {
			yes++
		} else {
			no++
		}
	}
	log.Printf("[geography] компаний=%d, российские=%d, иностранные=%d, не определено=%d, отчёт=%s", len(records), yes, no, unknown, report)
}
