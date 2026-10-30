package yopt

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"suppliers-parser/internal/core"
	"suppliers-parser/internal/models"

	"github.com/PuerkitoBio/goquery"
)

const (
	baseURL    = "https://yopt.org"
	startURL   = "https://yopt.org/postavshiki"
	sourceName = "yopt"
)

var (
	reINN  = regexp.MustCompile(`(\d{10,12})`)
	reOGRN = regexp.MustCompile(`(\d{13,15})`)
)

type Yopt struct {
	client *http.Client
}

func init() {
	core.Register(sourceName, func() core.Source {
		return &Yopt{
			client: &http.Client{Timeout: 20 * time.Second},
		}
	})
}

func (y *Yopt) HTTPClient() *http.Client { return y.client }

func (y *Yopt) Name() string { return sourceName }

func (y *Yopt) Parse(ctx context.Context) (<-chan models.Company, error) {
	out := make(chan models.Company)

	go func() {
		defer close(out)

		for page := 0; ; page++ {
			select {
			case <-ctx.Done():
				return
			default:
			}

			pageURL := fmt.Sprintf("%s?page=%d", startURL, page)
			doc, err := y.fetch(ctx, pageURL)
			if err != nil {
				log.Printf("[yopt] ошибка страницы %d: %v", page, err)
				if ctx.Err() != nil || strings.Contains(err.Error(), "status 404") {
					return
				}
				continue
			}

			cards := y.parseListing(doc)
			if len(cards) == 0 {
				return
			}

			log.Printf("[%s] страница %d: компаний=%d", sourceName, page, len(cards))
			for i, card := range cards {
				log.Printf("[%s] компания %d/%d: %s (%s)", sourceName, i+1, len(cards), card.Name, card.URL)
				select {
				case <-ctx.Done():
					return
				default:
				}

				company, err := y.parseCompany(ctx, card.URL, card.Name)
				if err != nil {
					log.Printf("[yopt] ошибка компании %s: %v", card.URL, err)
					if ctx.Err() != nil {
						return
					}
					continue
				}

				select {
				case <-ctx.Done():
					return
				case out <- company:
				}

				time.Sleep(300 * time.Millisecond)
			}
		}
	}()

	return out, nil
}

type card struct {
	Name string
	URL  string
}

func (y *Yopt) parseListing(doc *goquery.Document) []card {
	var cards []card
	doc.Find("div.field-content").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.title-stroka h2").Text())
		href, ok := s.Find("a.kartzagl").Attr("href")
		if !ok || href == "" {
			return
		}
		cards = append(cards, card{Name: name, URL: y.absURL(href)})
	})
	return cards
}

func (y *Yopt) parseCompany(ctx context.Context, companyURL, name string) (models.Company, error) {
	c := models.Company{
		Source: sourceName,
		URL:    companyURL,
		Name:   name,
	}

	doc, err := y.fetch(ctx, companyURL)
	if err != nil {
		return c, err
	}

	c.Category = parseCategory(doc)

	doc.Find("div.listInfo-supplier").Each(func(_ int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("span").First().Text())
		value := strings.TrimSpace(s.Find("strong").First().Text())
		switch {
		case strings.Contains(label, "Юр.лицо"):
			c.OfficialName = value
		case strings.Contains(label, "ИНН"):
			if m := reINN.FindStringSubmatch(value); m != nil {
				c.INN = m[1]
			}
		case strings.Contains(label, "ОГРН"):
			if m := reOGRN.FindStringSubmatch(value); m != nil {
				c.OGRN = m[1]
			}
		}
	})

	doc.Find("div.contn2 > div").Each(func(_ int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("span").First().Text())
		var value string
		if a := s.Find("a").First(); a.Length() > 0 {
			value = strings.TrimSpace(a.Text())
			if value == "" || value == "Показать" || value == "Открыть" {
				value, _ = a.Attr("href")
			}
		} else {
			spans := s.Find("span")
			if spans.Length() > 1 {
				value = strings.TrimSpace(spans.Eq(1).Text())
			}
		}
		value = strings.TrimSpace(value)

		switch {
		case strings.Contains(label, "Адрес"):
			c.Address = value
		case strings.Contains(label, "Телефон"):
			c.Phone = value
		case strings.Contains(label, "Email"):
			c.Email = value
		case strings.Contains(label, "Сайт"):
			c.Site = value
		case strings.Contains(label, "Телеграм"):
			c.Telegram = strings.ReplaceAll(value, "https://t.me/https://t.me/", "https://t.me/")
		case strings.Contains(label, "Вконтакте"):
			c.VK = value
		case strings.Contains(label, "Youtube"):
			c.Youtube = value
		}
	})

	c.PriceLists = y.parsePriceLists(ctx, doc)
	return c, nil
}

func (y *Yopt) parsePriceLists(ctx context.Context, doc *goquery.Document) []models.PriceList {
	var result []models.PriceList

	doc.Find("div.prikrfiles a").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}
		title := strings.TrimSpace(a.Text())
		if title == "" {
			title, _ = a.Attr("title")
		}
		if !isPriceListFile(href, title) {
			return
		}
		result = append(result, models.PriceList{
			Kind:  "file",
			Title: title,
			URL:   y.absURL(href),
		})
	})

	if href, ok := doc.Find("div.vsetovarypost a").Attr("href"); ok && href != "" {
		if inline := y.parseInlinePriceList(ctx, y.absURL(href)); inline != nil {
			result = append(result, *inline)
		}
	}
	return result
}

func (y *Yopt) parseInlinePriceList(ctx context.Context, listingURL string) *models.PriceList {
	productLinks, err := y.parseProductLinks(ctx, listingURL)
	if err != nil {
		log.Printf("[yopt] ошибка списка товаров %s: %v", listingURL, err)
		return nil
	}
	if len(productLinks) == 0 {
		return nil
	}

	var items []models.PriceItem
	for i, productURL := range productLinks {
		log.Printf("[%s] товар %d/%d: %s", sourceName, i+1, len(productLinks), productURL)
		item, err := y.parseProductCard(ctx, productURL)
		if err != nil {
			log.Printf("[yopt] ошибка товара %s: %v", productURL, err)
			if ctx.Err() != nil {
				return nil
			}
			return nil
		}
		if item.Name == "" {
			continue
		}
		items = append(items, item)
		time.Sleep(200 * time.Millisecond)
	}
	if len(items) == 0 {
		return nil
	}
	return &models.PriceList{
		Kind:  "inline",
		Title: "Товары на сайте",
		URL:   listingURL,
		Items: items,
	}
}

func (y *Yopt) parseProductLinks(ctx context.Context, listingURL string) ([]string, error) {
	var links []string
	seenPages := map[string]bool{}
	seenProducts := map[string]bool{}
	pageURL := listingURL
	for pageURL != "" {
		if seenPages[pageURL] {
			return nil, fmt.Errorf("цикл пагинации: %s", pageURL)
		}
		seenPages[pageURL] = true
		doc, err := y.fetch(ctx, pageURL)
		if err != nil {
			return nil, err
		}
		doc.Find("div.view-content div.views-row").Each(func(_ int, s *goquery.Selection) {
			href, ok := s.Find("div.title-stroka .title a").Attr("href")
			if !ok || href == "" {
				return
			}
			u := y.absURL(href)
			if !seenProducts[u] {
				seenProducts[u] = true
				links = append(links, u)
			}
		})
		log.Printf("[yopt] страница товаров=%d, всего ссылок=%d", len(seenPages), len(links))
		next := ""
		if href, ok := doc.Find("li.pager-next a, a[rel='next']").First().Attr("href"); ok {
			current, _ := url.Parse(pageURL)
			ref, err := url.Parse(href)
			if err != nil {
				return nil, err
			}
			target := current.ResolveReference(ref)
			if target.Host != current.Host || target.Path != current.Path {
				return nil, fmt.Errorf("неожиданная ссылка пагинации: %s", target)
			}
			next = target.String()
		}
		pageURL = next
		if pageURL != "" {
			timer := time.NewTimer(300 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return links, nil
}

func (y *Yopt) parseProductCard(ctx context.Context, productURL string) (models.PriceItem, error) {
	item := models.PriceItem{URL: productURL}
	doc, err := y.fetch(ctx, productURL)
	if err != nil {
		return item, err
	}

	item.Name = strings.TrimSpace(doc.Find("h1.title").First().Text())
	if item.Name == "" {
		item.Name = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	doc.Find("strong").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if strings.Contains(s.Parent().Text(), "Оптовая цена") {
			item.Price = strings.TrimSpace(s.Text())
			return false
		}
		return true
	})

	item.Desc = strings.TrimSpace(doc.Find("div.views-field-body .field-content").Text())
	item.Category = parseCategory(doc)
	return item, nil
}

func (y *Yopt) fetch(ctx context.Context, rawURL string) (doc *goquery.Document, err error) {
	started := time.Now()
	log.Printf("[%s] GET %s", sourceName, rawURL)
	defer func() {
		log.Printf("[%s] GET завершён за %s: %s, ошибка=%v", sourceName, time.Since(started).Round(time.Millisecond), rawURL, err)
	}()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := y.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, rawURL)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

func (y *Yopt) absURL(href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	b, _ := url.Parse(baseURL)
	r, _ := url.Parse(href)
	return b.ResolveReference(r).String()
}

func parseCategory(doc *goquery.Document) string {
	var cats []string
	doc.Find(`div[itemtype='https://schema.org/BreadcrumbList'] span[itemprop='itemListElement']`).Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Find("a[itemprop='item']").Attr("href")
		name := strings.TrimSpace(s.Find("span[itemprop='name']").Text())
		if name == "" || !strings.Contains(href, "/postavshiki/category/") {
			return
		}
		cats = append(cats, name)
	})
	return strings.Join(cats, " / ")
}

func isPriceListFile(href, title string) bool {
	lower := strings.ToLower(href + " " + title)
	for _, kw := range []string{"прайс", "price", "prays", "list"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	for _, ext := range []string{".xlsx", ".xls", ".csv"} {
		if strings.HasSuffix(strings.ToLower(href), ext) {
			return true
		}
	}
	return false
}
