package productcenter

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPacedTransportSpacesConcurrentRequests(t *testing.T) {
	var times []time.Time
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		times = append(times, time.Now())
		mu.Unlock()
	}))
	defer server.Close()
	const delay = 30 * time.Millisecond
	client := &http.Client{Transport: &pacedTransport{base: http.DefaultTransport, delay: delay}}
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := client.Get(server.URL)
			if err != nil {
				t.Error(err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if len(times) != 3 {
		t.Fatalf("got %d requests", len(times))
	}
	for i := 1; i < len(times); i++ {
		if gap := times[i].Sub(times[i-1]); gap < delay {
			t.Errorf("request gap %v < %v", gap, delay)
		}
	}
}

func TestPacedTransportStopsAfterBlock(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(status)
			}))
			defer server.Close()
			client := &http.Client{Transport: &pacedTransport{base: http.DefaultTransport}}
			for i := 0; i < 2; i++ {
				_, err := client.Get(server.URL)
				if !errors.Is(err, errBlocked) {
					t.Fatalf("expected blocked error, got %v", err)
				}
			}
			if calls.Load() != 1 {
				t.Errorf("sent %d requests after block", calls.Load())
			}
		})
	}
}

func TestPacedTransportCancelsWait(t *testing.T) {
	transport := &pacedTransport{next: time.Now().Add(time.Hour)}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = transport.RoundTrip(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
}
