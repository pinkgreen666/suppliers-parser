package leadership

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

type Options struct {
	Force      bool
	DryRun     bool
	TTL        time.Duration
	RetryAfter time.Duration
	Delay      time.Duration
}
type Report struct {
	Provider string   `json:"provider"`
	DryRun   bool     `json:"dry_run"`
	Selected int      `json:"selected"`
	Requests int      `json:"requests"`
	Records  []Record `json:"records"`
	Error    string   `json:"error,omitempty"`
}

func Run(ctx context.Context, store Store, provider Provider, companies []Company, options Options) (Report, error) {
	report := Report{Provider: "dadata", DryRun: options.DryRun, Selected: len(companies), Records: []Record{}}
	if options.TTL <= 0 || options.RetryAfter <= 0 || options.Delay < 0 {
		return report, fmt.Errorf("TTL и retry-after должны быть положительными, delay — неотрицательным")
	}
	if !options.DryRun && (store == nil || provider == nil) {
		return report, fmt.Errorf("не задано хранилище или API")
	}
	seen := map[string]Cache{}
	var next time.Time
	for index, c := range companies {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		record := Record{Company: c, AttemptedAt: time.Now().UTC()}
		id, err := Identify(c)
		if err != nil {
			record.Status = "invalid_identifier"
			record.Error = err.Error()
		} else {
			record.Key = id.Key()
			if options.DryRun {
				record.Status = "pending"
			} else {
				cached, used := seen[id.Key()]
				if !used {
					cached, err = store.GetCache(ctx, id.Key())
					if err != nil {
						return report, err
					}
				}
				now := time.Now()
				fresh := cached.Result != nil && now.Sub(cached.Result.CheckedAt) >= 0 && now.Sub(cached.Result.CheckedAt) < options.TTL
				retryBlocked := cached.Error != "" && now.Sub(cached.AttemptedAt) < options.RetryAfter
				if used || (!options.Force && (fresh && cached.Error == "" || retryBlocked)) {
					record.Cached = true
				} else {
					if wait := time.Until(next); wait > 0 {
						log.Printf("[enrich] пауза %s перед %s", wait.Round(time.Millisecond), id.Key())
						timer := time.NewTimer(wait)
						select {
						case <-ctx.Done():
							timer.Stop()
							return report, ctx.Err()
						case <-timer.C:
						}
					}
					log.Printf("[enrich] [%d/%d] запрос %s (%s)", index+1, len(companies), id.Key(), c.Name)
					result, lookupErr := provider.Lookup(ctx, id)
					report.Requests++
					next = time.Now().Add(options.Delay)
					if ctx.Err() != nil {
						return report, ctx.Err()
					}
					cached.AttemptedAt = time.Now().UTC()
					cached.Error = ""
					if lookupErr != nil {
						cached.Error = lookupErr.Error()
					} else {
						cached.Result = &result
					}
					if err := store.PutCache(ctx, id.Key(), cached); err != nil {
						return report, err
					}
					if lookupErr != nil {
						var apiErr *APIError
						if errors.As(lookupErr, &apiErr) && apiErr.Stop() {
							record.Status = "error"
							record.Error = cached.Error
							record.Result = cached.Result
							if record.Result != nil && !MatchesCompany(c, *record.Result) {
								record.Result = nil
							}
							saved, err := store.SaveRecord(ctx, record)
							if err != nil {
								return report, err
							}
							report.Records = append(report.Records, saved)
							return report, fmt.Errorf("обогащение остановлено: %w", lookupErr)
						}
					}
				}
				seen[id.Key()] = cached
				record.Result = cached.Result
				if cached.Error != "" {
					record.Status = "error"
					record.Error = cached.Error
				} else if cached.Result != nil {
					record.Status = cached.Result.Status
				}
				if cached.Result != nil && (cached.Result.INN != "" || cached.Result.OGRN != "") && !MatchesCompany(c, *cached.Result) {
					record.Status = "needs_review"
					record.Error = "ИНН/ОГРН ответа не совпадают с реквизитами компании"
					record.Result = nil
				}
			}
		}
		if !options.DryRun {
			saved, err := store.SaveRecord(ctx, record)
			if err != nil {
				return report, err
			}
			record = saved
		}
		report.Records = append(report.Records, record)
		log.Printf("[enrich] [%d/%d] id=%d %s: %s, кеш=%t", index+1, len(companies), c.ID, c.Name, record.Status, record.Cached)
	}
	return report, nil
}
