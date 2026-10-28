package runner

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"suppliers-parser/internal/core"
	"suppliers-parser/internal/storage"
)

type Runner struct {
	db            *storage.DB
	force         bool
	downloadsOnly bool
	delay         time.Duration
}

func New(db *storage.DB) *Runner {
	return &Runner{db: db, delay: 300 * time.Millisecond}
}

func (r *Runner) WithDownloadsOnly(v bool) *Runner { r.downloadsOnly = v; return r }

func (r *Runner) WithForce(f bool) *Runner          { r.force = f; return r }
func (r *Runner) WithDelay(d time.Duration) *Runner { r.delay = d; return r }

func (r *Runner) RunSources(ctx context.Context, sources []core.Source) map[string]core.Stats {
	results := make(map[string]core.Stats)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, src := range sources {
		wg.Add(1)
		go func(s core.Source) {
			defer wg.Done()

			log.Printf("[%s] старт", s.Name())
			stats, err := r.runSource(ctx, s)
			if err != nil {
				log.Printf("[%s] ошибка: %v", s.Name(), err)
			}
			mu.Lock()
			results[s.Name()] = stats
			mu.Unlock()

			log.Printf("[%s] готово %+v", s.Name(), stats)

		}(src)
	}

	wg.Wait()
	return results
}

func (r *Runner) runSource(ctx context.Context, src core.Source) (core.Stats, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stats := core.Stats{}

	if r.downloadsOnly {
		pending, err := r.db.PendingPriceFiles(src.Name())
		if err != nil {
			return stats, err
		}
		log.Printf("[%s] недостающих файлов: %d", src.Name(), len(pending))
		for _, pl := range pending {
			if ctx.Err() != nil {
				return stats, ctx.Err()
			}
			path, err := storage.DownloadPriceFile(ctx, src.HTTPClient(), pl.URL, fmt.Sprintf("%s_%d_", src.Name(), pl.CompanyID))
			if err != nil {
				stats.Failed++
				log.Printf("[%s] повторная загрузка %s: %v", src.Name(), pl.URL, err)
				continue
			}
			pl.FilePath = path
			if err := r.db.SavePriceList(pl); err != nil {
				stats.Failed++
				log.Printf("[%s] сохранение прайса: %v", src.Name(), err)
				continue
			}
			stats.SavedPrices++
		}
		return stats, ctx.Err()
	}
	log.Printf("[%s] парсинг сайта: force=%t", src.Name(), r.force)
	ch, err := src.Parse(ctx)
	if err != nil {
		return stats, err
	}

	for company := range ch {
		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		default:
		}

		exists, err := r.db.Exists(company.Source, company.URL)
		if err != nil {
			return stats, err
		}
		if exists && !r.force {
			log.Printf("[%s] пропуск существующей компании: %s", src.Name(), company.URL)
			stats.Skipped++
			continue
		}

		id, err := r.db.SaveCompany(company)
		if err != nil {
			stats.Failed++
			log.Printf("[%s] save company %s: %v", src.Name(), company.URL, err)
			continue
		}
		stats.SavedCompanies++
		log.Printf("[%s] компания сохранена: id=%d, %s, прайсов=%d", src.Name(), id, company.Name, len(company.PriceLists))

		// Persist every discovered URL before any potentially long download.
		for i := range company.PriceLists {
			company.PriceLists[i].CompanyID = id
			if err := r.db.SavePriceList(company.PriceLists[i]); err != nil {
				stats.Failed++
				return stats, err
			}
		}
		for i := range company.PriceLists {
			if ctx.Err() != nil {
				return stats, ctx.Err()
			}
			pl := &company.PriceLists[i]

			if pl.Kind == "file" && pl.URL != "" {
				prefix := company.Source + "_" + itoa(id) + "_"
				path, err := storage.DownloadPriceFile(ctx, src.HTTPClient(), pl.URL, prefix)
				if err != nil {
					stats.Failed++
					log.Printf("[%s] download %s: %v", src.Name(), pl.URL, err)
				} else {
					pl.FilePath = path
				}
			}

			pl.CompanyID = id
			if err := r.db.SavePriceList(*pl); err != nil {
				stats.Failed++
				log.Printf("[%s] save price: %v", src.Name(), err)
				continue
			}
			if pl.Kind != "file" || pl.FilePath != "" {
				stats.SavedPrices++
			}
		}

		select {
		case <-ctx.Done():
			return stats, ctx.Err()
		case <-time.After(r.delay):
		}
	}

	return stats, ctx.Err()

}

func itoa(i int64) string {
	return fmt.Sprintf("%d", i)
}
