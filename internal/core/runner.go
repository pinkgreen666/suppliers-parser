package core

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"suppliers-parser/internal/models"
	"suppliers-parser/internal/storage"
	"sync"
	"time"
)

type Runner struct {
	DB     *sql.DB
	Source Source
	Client *http.Client

	Workers   int
	BatchSize int
	Delay     time.Duration
	MaxPages  int
	Force     bool
}

func (r *Runner) Run() error {
	slog.Info("старт", "source", r.Source.Name(), "url", r.Source.StartURL())

	pageURL := r.Source.StartURL()
	page := 0

	for pageURL != "" {
		page++
		if r.MaxPages > 0 && page > r.MaxPages {
			break
		}
		slog.Info("страница", "num", page, "url", pageURL)

		cards, next, err := r.Source.ParseListing(pageURL)
		if err != nil {
			return fmt.Errorf("listing %s: %w", pageURL, err)
		}
		slog.Info("найдено карточек", "count", len(cards))

		if err := r.processCards(cards); err != nil {
			return err
		}

		pageURL = next
		time.Sleep(r.Delay)
	}

	return nil
}

func (r *Runner) processCards(cards []string) error {
	for i := 0; i < len(cards); i += r.BatchSize {
		end := i + r.BatchSize
		if end > len(cards) {
			end = len(cards)
		}
		batch := cards[i:end]

		slog.Info("батч", "from", i+1, "to", end, "of", len(cards))

		results := r.parseBatch(batch)
		if err := r.saveBatch(results); err != nil {
			slog.Error("save batch", "err", err)
		}
		time.Sleep(r.Delay)
	}
	return nil
}

func (r *Runner) parseBatch(cards []string) []models.Company {
	results := make([]models.Company, len(cards))
	sem := make(chan struct{}, r.Workers)
	var wg sync.WaitGroup

	for i, cardURL := range cards {
		if !r.Force {
			exists, _ := storage.Exists(r.DB, r.Source.Name(), cardURL)
			if exists {
				continue
			}
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, url string) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if rec := recover(); rec != nil {
					slog.Error("паника", "url", url, "rec", rec)
				}
			}()

			company, prices, err := r.Source.ParseCard(url)
			if err != nil {
				slog.Error("карточка", "url", url, "err", err)
				return
			}
			company.PriceLists = prices
			results[idx] = company

			time.Sleep(r.Delay)
		}(i, cardURL)
	}

	wg.Wait()
	return results
}

func (r *Runner) saveBatch(companies []models.Company) error {
	tx, err := r.DB.Begin()
	if err != nil {
		return nil
	}
	defer tx.Rollback()

	for i := range companies {
		c := &companies[i]
		if c.URL == "" {
			continue
		}

		id, err := storage.SaveCompanyTx(tx, *c)
		if err != nil {
			slog.Error("save company", "url", c.URL, "err", err)
			continue
		}

		for j := range c.PriceLists {
			c.PriceLists[j].CompanyID = id
			if err := storage.SavePriceListTx(tx, c.PriceLists[j]); err != nil {
				slog.Error("save pricelist", "url", c.URL, "err", err)
			}
		}
	}
	return tx.Commit()
}
