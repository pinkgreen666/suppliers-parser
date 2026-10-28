package productcenter

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

var errBlocked = errors.New("productcenter: access denied; stop parsing and retry only after access is restored")

// pacedTransport shares one request budget between parsing, redirects and downloads.
type pacedTransport struct {
	base    http.RoundTripper
	delay   time.Duration
	mu      sync.Mutex
	next    time.Time
	blocked bool
}

func (t *pacedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.blocked {
		return nil, errBlocked
	}
	if wait := time.Until(t.next); wait > 0 {
		log.Printf("[productcenter] пауза %s перед %s", wait.Round(time.Millisecond), req.URL)
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-timer.C:
		}
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	// Also pace failed requests; do not allow error loops to hammer the site.
	t.next = time.Now().Add(t.delay)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		t.blocked = true
		resp.Body.Close()
		return nil, fmt.Errorf("%w (HTTP %d)", errBlocked, resp.StatusCode)
	}
	return resp, nil
}
