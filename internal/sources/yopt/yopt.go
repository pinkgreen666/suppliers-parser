package yopt

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"suppliers-parser/internal/core"
	"suppliers-parser/internal/models"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	baseURL    = "https://yopt.org"
	startURL   = "https://yopt.org/postavshiki/category/oborudovanie-instrument/region/rossiya"
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
			doc, err := y.fetch(pageURL)
			if err != nil {
				log.Printf("yopt: listing %d: %v", page, err)
				if strings.Contains(err.Error(), "status 404") {
					return
				}
				continue
			}

			cards := y.parseListing(doc)
			if len(cards) == 0 {
				return
			}

			for _, card := range cards {
				select {
				case <-ctx.Done():
					return
				default:
				}

				company, err := y.parseCompany(card.URL, card.Name)
				if err != nil {
					log.Printf("yopt: company %s: %v", card.URL, err)
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

func (y *Yopt) parseCompany(companyURL, name string) (models.Company, error) {
	c := models.Company{
		Source: sourceName,
		URL:    companyURL,
		Name:   name,
	}

	doc, err := y.fetch(companyURL)
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

	c.PriceLists = y.parsePriceLists(doc)
	return c, nil
}

func (y *Yopt) parsePriceLists(doc *goquery.Document) []models.PriceList {
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
		if inline := y.parseInlinePriceList(y.absURL(href)); inline != nil {
			result = append(result, *inline)
		}
	}
	return result
}

func (y *Yopt) parseInlinePriceList(listingURL string) *models.PriceList {
	productLinks, err := y.parseProductLinks(listingURL)
	if err != nil {
		log.Printf("yopt: inline listing %s: %v", listingURL, err)
		return nil
	}
	if len(productLinks) == 0 {
		return nil
	}

	var items []models.PriceItem
	for _, productURL := range productLinks {
		item, err := y.parseProductCard(productURL)
		if err != nil {
			log.Printf("yopt: product %s: %v", productURL, err)
			continue
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

func (y *Yopt) parseProductLinks(listingURL string) ([]string, error) {
	doc, err := y.fetch(listingURL)
	if err != nil {
		return nil, err
	}
	var links []string
	doc.Find("div.view-content div.views-row").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Find("div.title-stroka .title a").Attr("href")
		if !ok || href == "" {
			return
		}
		links = append(links, y.absURL(href))
	})
	return links, nil
}

func (y *Yopt) parseProductCard(productURL string) (models.PriceItem, error) {
	item := models.PriceItem{URL: productURL}
	doc, err := y.fetch(productURL)
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

func (y *Yopt) fetch(rawURL string) (*goquery.Document, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
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
