// Package suplbiz reads the public endpoints used by the Supl.biz catalog.
package suplbiz

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"suppliers-parser/internal/core"
	"suppliers-parser/internal/models"
)

const sourceName = "suplbiz"

type SuplBiz struct {
	client      *http.Client
	baseURL     string
	delay       time.Duration
	nextRequest time.Time
}

func init() { core.Register(sourceName, func() core.Source { return newSource() }) }

func newSource() *SuplBiz {
	// The site sets language/session cookies before redirecting to the same URL.
	jar, _ := cookiejar.New(nil)
	return &SuplBiz{client: &http.Client{Timeout: 30 * time.Second, Jar: jar}, baseURL: "https://supl.biz", delay: time.Second}
}

func (s *SuplBiz) Name() string             { return sourceName }
func (s *SuplBiz) HTTPClient() *http.Client { return s.client }

type supplier struct {
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	HashID  string `json:"hash_id"`
	INN     string `json:"inn"`
	Address string `json:"address"`
	Origin  struct {
		Title string `json:"title"`
	} `json:"origin_ru"`
}

type supplierPage struct {
	Hits       []supplier `json:"hits"`
	TotalCount struct {
		Value    int    `json:"value"`
		Relation string `json:"relation"`
	} `json:"total_count"`
}

func (s *SuplBiz) listing(ctx context.Context, page int) (supplierPage, error) {
	return s.categoryListing(ctx, 0, page)
}

func (s *SuplBiz) categoryListing(ctx context.Context, category int64, page int) (supplierPage, error) {
	var result supplierPage
	endpoint := fmt.Sprintf("/api/monolith/suppliers-catalog/search/?city=russian-federation&page=%d&size=8", page)
	if category > 0 {
		endpoint += "&category=" + strconv.FormatInt(category, 10)
	}
	err := s.get(ctx, endpoint, &result)
	return result, err
}

type catalogCategory struct {
	ID       int64             `json:"id"`
	Children []catalogCategory `json:"children"`
}

func (s *SuplBiz) childCategories(ctx context.Context, id int64) ([]catalogCategory, error) {
	if id == 0 {
		var categories []catalogCategory
		err := s.get(ctx, "/api/monolith/suppliers-catalog/menu/", &categories)
		return categories, err
	}
	var category catalogCategory
	err := s.get(ctx, fmt.Sprintf("/api/monolith/suppliers-catalog/categories/%d/menu/", id), &category)
	return category.Children, err
}

func (s *SuplBiz) Parse(ctx context.Context) (<-chan models.Company, error) {
	first, err := s.listing(ctx, 0)
	if err != nil {
		return nil, err
	}
	out := make(chan models.Company)
	go func() {
		defer close(out)
		seen := make(map[int64]bool)
		seenCategories := make(map[int64]bool)
		queue := []int64{0}
		for len(queue) > 0 {
			category := queue[0]
			queue = queue[1:]
			if seenCategories[category] {
				continue
			}
			seenCategories[category] = true
			batch := first
			if category != 0 {
				batch, err = s.categoryListing(ctx, category, 0)
				if err != nil {
					log.Printf("[suplbiz] категория %d: %v", category, err)
					return
				}
			}
			// The general search window ends at 10,000. Traverse narrower
			// categories instead of silently treating that window as the whole site.
			if batch.TotalCount.Relation == "gte" {
				children, err := s.childCategories(ctx, category)
				if err != nil {
					log.Printf("[suplbiz] подкатегории %d: %v", category, err)
					return
				}
				for _, child := range children {
					if child.ID > 0 {
						queue = append(queue, child.ID)
					}
				}
				if len(children) > 0 {
					continue
				}
				log.Printf("[suplbiz] категория %d превышает окно поиска, подкатегорий нет; полнота ограничена сайтом", category)
			}
			pageSeen := make(map[int64]bool)
			for page := 0; ; page++ {
				if page > 0 {
					batch, err = s.categoryListing(ctx, category, page)
					if err != nil {
						log.Printf("[suplbiz] ошибка каталога, страница %d: %v", page, err)
						return
					}
				}
				if len(batch.Hits) == 0 {
					break
				}
				log.Printf("[suplbiz] страница %d: компаний=%d", page, len(batch.Hits))
				added := 0
				for _, card := range batch.Hits {
					if card.ID <= 0 || pageSeen[card.ID] {
						continue
					}
					pageSeen[card.ID] = true
					added++
					if seen[card.ID] {
						continue
					}
					seen[card.ID] = true
					company, err := s.parseCompany(ctx, card)
					if ctx.Err() != nil {
						return
					}
					if err != nil {
						log.Printf("[suplbiz] ошибка компании %d: %v", card.ID, err)
						continue
					}
					select {
					case out <- company:
					case <-ctx.Done():
						return
					}
				}
				if added == 0 {
					log.Printf("[suplbiz] каталог повторяет уже полученные компании; остановка")
					break
				}
				if batch.TotalCount.Relation == "eq" && batch.TotalCount.Value > 0 && (page+1)*8 >= batch.TotalCount.Value {
					break
				}
			}
		}
	}()
	return out, nil
}

type named struct {
	Title string `json:"title"`
	Name  string `json:"name"`
}
type profile struct {
	ID            int64   `json:"id"`
	Title         string  `json:"title"`
	Address       string  `json:"address"`
	Origin        named   `json:"origin"`
	Rubrics       []named `json:"rubrics"`
	Phone         string  `json:"phone"`
	Email         string  `json:"email"`
	Site          string  `json:"site"`
	HasRequisites bool    `json:"has_requisites"`
}

func (s *SuplBiz) parseCompany(ctx context.Context, card supplier) (models.Company, error) {
	c := models.Company{Source: sourceName, URL: s.baseURL + fmt.Sprintf("/profiles/%d/", card.ID), Name: card.Title, INN: card.INN, Address: join(card.Origin.Title, card.Address)}
	if card.HashID != "" {
		c.URL = s.baseURL + "/profiles/h-" + url.PathEscape(card.HashID) + "/"
	}
	log.Printf("[suplbiz] компания %d: %s", card.ID, card.Title)
	var p profile
	root := fmt.Sprintf("/api/monolith/new-profiles/%d/", card.ID)
	if err := s.get(ctx, root, &p); err != nil {
		return c, err
	}
	if p.ID != card.ID || strings.TrimSpace(p.Title) == "" {
		return c, fmt.Errorf("некорректный профиль %d", card.ID)
	}
	c.Name, c.Phone, c.Email, c.Site = p.Title, fullPhone(p.Phone), p.Email, p.Site
	if address := join(p.Origin.Title, p.Address); address != "" {
		c.Address = address
	}
	c.Category = names(p.Rubrics)
	if c.Phone == "" {
		var contacts struct {
			Phone string `json:"phone"`
		}
		if err := s.get(ctx, fmt.Sprintf("/api/monolith/supplier_contacts/%d/", card.ID), &contacts); err != nil {
			log.Printf("[suplbiz] контакты %d: %v", card.ID, err)
		} else {
			c.Phone = fullPhone(contacts.Phone)
		}
	}
	if p.HasRequisites {
		var req struct {
			INN          string `json:"inn"`
			OGRN         string `json:"ogrn"`
			LegalAddress string `json:"legal_address"`
		}
		if err := s.get(ctx, root+"requisites/", &req); err != nil {
			log.Printf("[suplbiz] реквизиты %d: %v", card.ID, err)
		} else {
			if req.INN != "" {
				c.INN = req.INN
			}
			c.OGRN = req.OGRN
			if c.Address == "" {
				c.Address = req.LegalAddress
			}
		}
	}
	var about struct {
		CompanyName        string `json:"company_name"`
		AdditionalContacts string `json:"additional_contacts"`
	}
	if err := s.get(ctx, root+"about/", &about); err != nil {
		log.Printf("[suplbiz] описание %d: %v", card.ID, err)
	} else {
		c.OfficialName = about.CompanyName
		// Only supplier-provided links, never the platform's footer contacts.
		for _, v := range strings.Fields(about.AdditionalContacts) {
			u, err := url.Parse(strings.Trim(v, ",;"))
			if err != nil || u.Scheme != "https" && u.Scheme != "http" {
				continue
			}
			switch strings.ToLower(u.Hostname()) {
			case "t.me", "telegram.me":
				c.Telegram = u.String()
			case "vk.com", "www.vk.com":
				c.VK = u.String()
			case "youtube.com", "www.youtube.com", "youtu.be":
				c.Youtube = u.String()
			}
		}
	}
	prices, err := s.parseProducts(ctx, card.ID)
	if err != nil {
		return c, fmt.Errorf("товары компании %d: %w", card.ID, err)
	}
	c.PriceLists = prices
	return c, nil
}

type product struct {
	ID           int64           `json:"id"`
	UserID       int64           `json:"user_id"`
	Title        string          `json:"title"`
	Slug         string          `json:"slug"`
	Description  string          `json:"description"`
	Price        json.RawMessage `json:"price"`
	Currency     string          `json:"currency"`
	PriceDetails string          `json:"price_details"`
	Breadcrumbs  []named         `json:"breadcrumbs"`
	Attachments  []struct {
		URL      string `json:"url"`
		FileName string `json:"file_name"`
	} `json:"attachments"`
}

func (s *SuplBiz) parseProducts(ctx context.Context, id int64) ([]models.PriceList, error) {
	var items []models.PriceItem
	var files []models.PriceList
	seen := make(map[int64]bool)
	seenFiles := make(map[string]bool)
	for page := 1; ; page++ {
		var batch struct {
			Total int `json:"total"`
			Hits  []struct {
				ID int64 `json:"id"`
			} `json:"hits"`
		}
		if err := s.get(ctx, fmt.Sprintf("/api/proposals/proposals/users/%d/search/?page=%d&size=12&sort=default_asc", id, page), &batch); err != nil {
			return nil, err
		}
		if len(batch.Hits) == 0 {
			if len(seen) < batch.Total {
				return nil, fmt.Errorf("пустая страница товаров до конца каталога")
			}
			break
		}
		added := 0
		for _, hit := range batch.Hits {
			if hit.ID <= 0 || seen[hit.ID] {
				continue
			}
			var p product
			if err := s.get(ctx, fmt.Sprintf("/api/proposals/proposals/%d/", hit.ID), &p); err != nil {
				return nil, err
			}
			if p.ID != hit.ID || p.UserID != id || strings.TrimSpace(p.Title) == "" {
				return nil, fmt.Errorf("некорректный товар %d компании %d", hit.ID, id)
			}
			seen[p.ID] = true
			added++
			productURL := s.baseURL + fmt.Sprintf("/proposals/%d/", p.ID)
			if p.Slug != "" {
				productURL = s.baseURL + "/" + url.PathEscape(p.Slug) + fmt.Sprintf("-p%d/", p.ID)
			}
			items = append(items, models.PriceItem{Name: p.Title, URL: productURL, Price: price(p), Desc: p.Description, Category: names(p.Breadcrumbs)})
			for _, a := range p.Attachments {
				u, err := url.Parse(a.URL)
				if err != nil {
					continue
				}
				base, _ := url.Parse(s.baseURL)
				u = base.ResolveReference(u)
				ext := strings.ToLower(path.Ext(u.Path))
				if (u.Scheme != "http" && u.Scheme != "https") || seenFiles[u.String()] || (ext != ".xlsx" && ext != ".xls" && ext != ".csv" && ext != ".pdf") {
					continue
				}
				seenFiles[u.String()] = true
				files = append(files, models.PriceList{Kind: "file", Title: a.FileName, URL: u.String()})
			}
		}
		if added == 0 {
			return nil, fmt.Errorf("повтор страницы товаров %d", page)
		}
		if len(seen) >= batch.Total {
			break
		}
	}
	if len(items) > 0 {
		files = append(files, models.PriceList{Kind: "inline", Title: "Товары на сайте", URL: s.baseURL + fmt.Sprintf("/proposals/supplier/%d/", id), Items: items})
	}
	return files, nil
}

func price(p product) string {
	v := strings.TrimSpace(string(p.Price))
	if strings.HasPrefix(v, "\"") {
		var decoded string
		if json.Unmarshal(p.Price, &decoded) == nil {
			v = decoded
		}
	}
	if v == "" || v == "null" {
		return strings.TrimSpace(p.PriceDetails)
	}
	return strings.TrimSpace(strings.Join([]string{v, p.Currency, p.PriceDetails}, " "))
}

func fullPhone(v string) string {
	if strings.Contains(v, "...") || strings.Contains(v, "…") {
		return ""
	}
	return strings.TrimSpace(v)
}
func join(values ...string) string {
	var parts []string
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			parts = append(parts, v)
		}
	}
	return strings.Join(parts, ", ")
}
func names(values []named) string {
	var parts []string
	for _, v := range values {
		n := v.Title
		if n == "" {
			n = v.Name
		}
		if n != "" {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, " / ")
}

func (s *SuplBiz) get(ctx context.Context, endpoint string, dest any) error {
	for attempt := 0; attempt < 3; attempt++ {
		if wait := time.Until(s.nextRequest); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
		s.nextRequest = time.Now().Add(s.delay)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+endpoint, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Custom-Accept-Language", "ru")
		log.Printf("[suplbiz] GET %s", req.URL)
		resp, err := s.client.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt == 2 {
				return fmt.Errorf("status %d for %s", resp.StatusCode, req.URL)
			}
			backoff := time.Duration(attempt+1) * 5 * time.Second
			if seconds, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && seconds > 0 {
				backoff = time.Duration(seconds) * time.Second
			}
			s.nextRequest = time.Now().Add(backoff)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("status %d for %s", resp.StatusCode, req.URL)
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(dest)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("JSON %s: %w", req.URL, err)
		}
		return nil
	}
	return fmt.Errorf("не удалось получить %s", endpoint)
}
