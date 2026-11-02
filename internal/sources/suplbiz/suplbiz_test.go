package suplbiz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"suppliers-parser/internal/core"
)

func TestCompanyAndProductPagination(t *testing.T) {
	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/monolith/new-profiles/7/":
			fmt.Fprint(w, `{"id":7,"title":"Company","phone":"7999...","email":"sales@example.org","site":"https://example.org","origin":{"title":"Томск"},"address":"ул. Ленина, 1","rubrics":[{"title":"Бетон"}],"has_requisites":true}`)
		case "/api/monolith/supplier_contacts/7/":
			fmt.Fprint(w, `{"phone":"+79991234567"}`)
		case "/api/monolith/new-profiles/7/requisites/":
			fmt.Fprint(w, `{"inn":"7017474450","ogrn":"1234567890123"}`)
		case "/api/monolith/new-profiles/7/about/":
			fmt.Fprint(w, `{"company_name":"ООО Company","additional_contacts":"https://t.me/company https://vk.com/company"}`)
		case "/api/proposals/proposals/users/7/search/":
			page := r.URL.Query().Get("page")
			pages = append(pages, page)
			if page == "1" {
				fmt.Fprint(w, `{"total":2,"hits":[{"id":11}]}`)
			} else {
				fmt.Fprint(w, `{"total":2,"hits":[{"id":11},{"id":12}]}`)
			}
		case "/api/proposals/proposals/11/", "/api/proposals/proposals/12/":
			id := 11
			if strings.Contains(r.URL.Path, "/12/") {
				id = 12
			}
			fmt.Fprintf(w, `{"id":%d,"user_id":7,"title":"Раствор","slug":"rastvor","description":"Полное описание","price":"0.00","currency":"RUB","price_details":"за кг","breadcrumbs":[{"name":"Материалы"},{"name":"Бетон"}],"attachments":[{"url":"/price.xlsx","file_name":"Прайс"}]}`, id)
		default:
			t.Errorf("unexpected request: %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := newSource()
	s.baseURL = server.URL
	s.delay = 0
	c, err := s.parseCompany(context.Background(), supplier{ID: 7, HashID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Source != "suplbiz" || c.URL != server.URL+"/profiles/h-abc/" || c.OfficialName != "ООО Company" || c.INN != "7017474450" || c.OGRN != "1234567890123" || c.Phone != "+79991234567" || c.Email != "sales@example.org" || c.Site != "https://example.org" || c.Address != "Томск, ул. Ленина, 1" || c.Category != "Бетон" || c.Telegram != "https://t.me/company" || c.VK != "https://vk.com/company" {
		t.Fatalf("company: %+v", c)
	}
	if len(c.PriceLists) != 2 || c.PriceLists[0].Kind != "file" || len(c.PriceLists[1].Items) != 2 {
		t.Fatalf("prices: %+v", c.PriceLists)
	}
	item := c.PriceLists[1].Items[0]
	if item.Desc != "Полное описание" || item.Price != "0.00 RUB за кг" || item.Category != "Материалы / Бетон" {
		t.Fatalf("item: %+v", item)
	}
	if strings.Join(pages, ",") != "1,2" {
		t.Fatalf("pages: %v", pages)
	}
}

func TestProductsRejectIncompleteCatalog(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"blocked", `denied`, 403}, {"invalid JSON", `<html>challenge</html>`, 200}, {"empty before total", `{"total":2,"hits":[]}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer server.Close()
			s := newSource()
			s.baseURL = server.URL
			s.delay = 0
			if _, err := s.parseProducts(context.Background(), 7); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestParseStopsOnRepeatedCatalog(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "suppliers-catalog") {
			calls++
			fmt.Fprint(w, `{"hits":[{"id":7,"title":"Company"}]}`)
			return
		}
		if r.URL.Path == "/api/monolith/new-profiles/7/" {
			fmt.Fprint(w, `{"id":7,"title":"Company","phone":"+79991234567"}`)
			return
		}
		if strings.Contains(r.URL.Path, "/about/") {
			fmt.Fprint(w, `{}`)
			return
		}
		fmt.Fprint(w, `{"total":0,"hits":[]}`)
	}))
	defer server.Close()
	s := newSource()
	s.baseURL = server.URL
	s.delay = 0
	out, err := s.Parse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for range out {
		count++
	}
	if count != 1 || calls != 2 {
		t.Fatalf("companies=%d listing calls=%d", count, calls)
	}
}

func TestCookiesAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := r.Cookie("language"); err != nil {
			http.SetCookie(w, &http.Cookie{Name: "language", Value: "ru", Path: "/"})
			http.Redirect(w, r, r.URL.String(), http.StatusFound)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	s := newSource()
	s.baseURL = server.URL
	s.delay = 0
	var v any
	if err := s.get(context.Background(), "/", &v); err != nil {
		t.Fatal(err)
	}
	s.nextRequest = time.Now().Add(time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.get(ctx, "/", &v); err != context.Canceled {
		t.Fatalf("got %v", err)
	}
	registered, err := core.Get("suplbiz")
	if err != nil || registered.Name() != "suplbiz" {
		t.Fatalf("registration: %v", err)
	}
}

// Explicitly opt in: this test makes real, paced requests without touching SQLite.
func TestLiveCompanies(t *testing.T) {
	if os.Getenv("SUPLBIZ_LIVE_TEST") != "1" {
		t.Skip("set SUPLBIZ_LIVE_TEST=1 to check live companies")
	}
	s := newSource()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for _, id := range []int64{993705, 2241385, 2271245} {
		c, err := s.parseCompany(ctx, supplier{ID: id})
		if err != nil {
			t.Fatal(err)
		}
		items := 0
		for _, p := range c.PriceLists {
			items += len(p.Items)
		}
		if c.Name == "" || c.Phone == "" || items == 0 {
			t.Fatalf("incomplete company %d: name=%q phone=%t items=%d", id, c.Name, c.Phone != "", items)
		}
		data, _ := json.Marshal(map[string]any{"id": id, "name": c.Name, "inn": c.INN, "has_phone": c.Phone != "", "has_email": c.Email != "", "has_site": c.Site != "", "category": c.Category, "items": items})
		t.Log(string(data))
	}
}

func TestLiveCatalog(t *testing.T) {
	if os.Getenv("SUPLBIZ_LIVE_TEST") != "1" {
		t.Skip("set SUPLBIZ_LIVE_TEST=1 to check live catalog")
	}
	s := newSource()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	seen := make(map[int64]bool)
	for _, category := range []int64{0, 9403} {
		children, err := s.childCategories(ctx, category)
		if err != nil || len(children) == 0 {
			t.Fatalf("category %d: children=%d err=%v", category, len(children), err)
		}
		t.Logf("category=%d children=%d", category, len(children))
	}
	for page := 0; page < 2; page++ {
		batch, err := s.categoryListing(ctx, 9404, page)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Hits) == 0 {
			t.Fatalf("empty page %d", page)
		}
		for _, card := range batch.Hits {
			if card.ID <= 0 || card.HashID == "" || seen[card.ID] {
				t.Fatalf("invalid/repeated supplier: %+v", card)
			}
			seen[card.ID] = true
		}
		t.Logf("page=%d companies=%d total=%d", page, len(batch.Hits), batch.TotalCount.Value)
	}
}

func TestParseSplitsCappedCategories(t *testing.T) {
	visited := make(map[string]bool)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "suppliers-catalog/search"):
			category := r.URL.Query().Get("category")
			visited[category] = true
			if category == "" || category == "10" {
				fmt.Fprint(w, `{"hits":[],"total_count":{"value":10000,"relation":"gte"}}`)
			} else {
				fmt.Fprint(w, `{"hits":[{"id":7}],"total_count":{"value":1,"relation":"eq"}}`)
			}
		case r.URL.Path == "/api/monolith/suppliers-catalog/menu/":
			fmt.Fprint(w, `[{"id":10},{"id":20}]`)
		case r.URL.Path == "/api/monolith/suppliers-catalog/categories/10/menu/":
			fmt.Fprint(w, `{"id":10,"children":[{"id":11}]}`)
		case r.URL.Path == "/api/monolith/new-profiles/7/":
			fmt.Fprint(w, `{"id":7,"title":"Company","phone":"+79991234567"}`)
		case strings.Contains(r.URL.Path, "/about/"):
			fmt.Fprint(w, `{}`)
		case strings.Contains(r.URL.Path, "/search/"):
			fmt.Fprint(w, `{"total":0,"hits":[]}`)
		default:
			t.Errorf("unexpected request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s := newSource()
	s.baseURL = server.URL
	s.delay = 0
	out, err := s.Parse(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for range out {
		count++
	}
	if count != 1 || !visited["10"] || !visited["11"] || !visited["20"] {
		t.Fatalf("count=%d visited=%v", count, visited)
	}
}

func TestPrice(t *testing.T) {
	for _, tc := range []struct{ raw, currency, details, want string }{
		{`null`, "RUB", "Цена по запросу", "Цена по запросу"},
		{`3100`, "RUB", "Цена указана ОТ", "3100 RUB Цена указана ОТ"},
		{`"0.00"`, "RUB", "", "0.00 RUB"},
	} {
		if got := price(product{Price: json.RawMessage(tc.raw), Currency: tc.currency, PriceDetails: tc.details}); got != tc.want {
			t.Errorf("price(%s)=%q, want %q", tc.raw, got, tc.want)
		}
	}
}
