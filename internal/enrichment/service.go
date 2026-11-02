package enrichment

import (
	"context"
	"log"
	"sync"
	"time"

	"suppliers-parser/internal/categorize"
	"suppliers-parser/internal/leadership"
	"suppliers-parser/internal/storage"
)

// Service serializes lookups across adapters and preserves rate limits between companies.
type Service struct {
	DB       *storage.DB
	Matcher  *categorize.Matcher
	Provider leadership.Provider
	mu       sync.Mutex
	stopped  bool
	next     time.Time
}

func (s *Service) Lookup(ctx context.Context, id leadership.Identifier) (leadership.Result, error) {
	if wait := time.Until(s.next); wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return leadership.Result{}, ctx.Err()
		case <-t.C:
		}
	}
	r, err := s.Provider.Lookup(ctx, id)
	s.next = time.Now().Add(time.Second)
	return r, err
}
func (s *Service) Process(ctx context.Context, c leadership.Company) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.DB.CategorizeCompany(ctx, c.ID, s.Matcher)
	if err != nil {
		return err
	}
	log.Printf("[categorize] company_id=%d: товаров=%d, категорий=%d, статус=%s", c.ID, len(r.Products), len(r.Categories), r.Status)
	if s.Provider == nil || s.stopped {
		return nil
	}
	_, err = leadership.Run(ctx, s.DB, s, []leadership.Company{c}, leadership.Options{TTL: 30 * 24 * time.Hour, RetryAfter: time.Hour})
	if err != nil {
		// Run returns for fatal API or storage errors; ordinary lookup errors are persisted.
		s.stopped = true
		log.Printf("[enrich] поиск остановлен до следующего запуска: %v", err)
	}
	return err
}

// ProcessFiles refreshes only categories after downloads, without repeating API lookup.
func (s *Service) ProcessFiles(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.DB.CategorizeCompany(ctx, id, s.Matcher)
	if err == nil {
		log.Printf("[categorize] company_id=%d: после загрузки файлов, товаров=%d, категорий=%d, статус=%s", id, len(r.Products), len(r.Categories), r.Status)
	}
	return err
}
