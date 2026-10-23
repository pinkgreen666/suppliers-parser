package runner

import (
	"context"
	"fmt"
	"log"
	"suppliers-parser/internal/core"
	"suppliers-parser/internal/storage"
	"sync"
	"time"
)

type Runner struct {
	db    *storage.DB
	force bool
	delay time.Duration
}

func New(db *storage.DB) *Runner {
	return &Runner{db: db, delay: 300 * time.Millisecond}
}

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
	stats := core.Stats{}

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
		if err == nil && exists && !r.force {
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

		for i := range company.PriceLists {
			pl := &company.PriceLists[i]

			if pl.Kind == "file" && pl.URL != "" {
				prefix := company.Source + "_" + itoa(id) + "_"
				path, err := storage.DownloadPriceFile(src.HTTPClient(), pl.URL, prefix)
				if err != nil {
					log.Printf("[%s] download %s: %v", src.Name(), pl.URL, err)
				} else {
					pl.FilePath = path
				}
			}

			pl.CompanyID = id
			if err := r.db.SavePriceList(*pl); err != nil {
				log.Printf("[%s] save price: %v", src.Name(), err)
				continue
			}
			stats.SavedPrices++
		}

		time.Sleep(r.delay)
	}

	return stats, nil

}

func itoa(i int64) string {
	return fmt.Sprintf("%d", i)
}
