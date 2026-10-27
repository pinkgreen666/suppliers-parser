package productcenter

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
	baseURL    = "https://productcenter.ru"
	startURL   = "https://productcenter.ru/producers"
	sourceName = "productcenter"
)

var (
	reINN  = regexp.MustCompile(`(\d{10,12})`)
	reOGRN = regexp.MustCompile(`(\d{13,15})`)
)

type ProductCenter struct {
	client *http.Client
}

func init() {
	core.Register(sourceName, func() core.Source {
		return &ProductCenter{
			client: &http.Client{
				Timeout: 20 * time.Second,
				Transport: &http.Transport{
					MaxIdleConns:        50,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			},
		}
	})
}

func (p *ProductCenter) HTTPClient() *http.Client { return p.client }

func (p *ProductCenter) Name() string { return sourceName }

func (p *ProductCenter) Parse(ctx context.Context) (<-chan models.Company, error) {
	out := make(chan models.Company)

	go func() {
		defer close(out)

		for page := 1; ; page++ {
			select {
			case <-ctx.Done():
				return
			default:
			}

			pageURL := fmt.Sprintf("%s?page=%d", startURL, page)
			doc, err := p.fetch(pageURL)
			if err != nil {
				log.Printf("productcenter: listing %d: %v", page, err)
				if strings.Contains(err.Error(), "status 404") {
					return
				}
				continue
			}

			cards := p.parseListing(doc)
			if len(cards) == 0 {
				return
			}

			for _, card := range cards {
				select {
				case <-ctx.Done():
					return
				default:
				}

				company, err := p.parseCompany(card.URL, card.Name)
				if err != nil {
					log.Printf("productcenter: company %s: %v", card.URL, err)
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

func (p *ProductCenter) parseListing(doc *goquery.Document) []card {
	var cards []card
	doc.Find("div.card_item.firm").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.item_descriptor").First().Text())
		href, ok := s.Find("a[href^='/producers/']").First().Attr("href")
		if !ok || href == "" || name == "" {
			return
		}
		cards = append(cards, card{Name: name, URL: p.absURL(href)})
	})
	return cards
}

func (p *ProductCenter) parseCompany(companyURL, name string) (models.Company, error) {
	c := models.Company{
		Source: sourceName,
		URL:    companyURL,
		Name:   name,
	}

	doc, err := p.fetch(companyURL)
	if err != nil {
		return c, err
	}

	c.Category = parseCategory(doc)

	doc.Find("div.tab_content.tc_contacts table.company_data tr").Each(func(_ int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("td").First().Text())
		value := strings.TrimSpace(s.Find("td").Eq(1).Text())
		switch {
		case strings.Contains(label, "Наименование") || strings.Contains(label, "Название"):
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

	doc.Find("div.tab_content.tc_contacts table:first-of-type tr").Each(func(_ int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("td").First().Text())
		value := strings.TrimSpace(s.Find("td").Eq(1).Text())
		switch {
		case strings.Contains(label, "Адрес"):
			c.Address = value
		case strings.Contains(label, "Телефон"):
			c.Phone = value
		case strings.Contains(label, "WhatsApp"):
			if c.Phone == "" {
				c.Phone = value
			}
		case strings.Contains(label, "Telegram"):
			c.Telegram = strings.TrimPrefix(value, "https://t.me/")
			if !strings.HasPrefix(c.Telegram, "https://") {
				c.Telegram = "https://t.me/" + c.Telegram
			}
		case strings.Contains(label, "Электронная почта") || strings.Contains(label, "Email"):
			if a := s.Find("a[href^='mailto:']").First(); a.Length() > 0 {
				c.Email, _ = a.Attr("href")
				c.Email = strings.TrimPrefix(c.Email, "mailto:")
			} else {
				c.Email = value
			}
		case strings.Contains(label, "Инстаграм") || strings.Contains(label, "Instagram"):
			// Could store if needed
		case strings.Contains(label, "Официальный сайт") || strings.Contains(label, "Сайт"):
			if a := s.Find("a[itemprop='url']").First(); a.Length() > 0 {
				c.Site, _ = a.Attr("href")
			} else if a := s.Find("a").First(); a.Length() > 0 {
				c.Site, _ = a.Attr("href")
			} else {
				c.Site = value
			}
		}
	})

	c.PriceLists = p.parsePriceLists(doc, companyURL)
	return c, nil
}

func (p *ProductCenter) parsePriceLists(doc *goquery.Document, companyURL string) []models.PriceList {
	var result []models.PriceList

	doc.Find("a[href^='/uploads/']").Each(func(_ int, a *goquery.Selection) {
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
			URL:   p.absURL(href),
		})
	})

	if href, ok := doc.Find("a[href^='/products/producer-']").First().Attr("href"); ok && href != "" {
		if inline := p.parseInlinePriceList(p.absURL(href)); inline != nil {
			result = append(result, *inline)
		}
	}
	return result
}

func (p *ProductCenter) parseInlinePriceList(listingURL string) *models.PriceList {
	productLinks, err := p.parseProductLinks(listingURL)
	if err != nil {
		log.Printf("productcenter: inline listing %s: %v", listingURL, err)
		return nil
	}
	if len(productLinks) == 0 {
		return nil
	}

	var items []models.PriceItem
	for _, productURL := range productLinks {
		item, err := p.parseProductCard(productURL)
		if err != nil {
			log.Printf("productcenter: product %s: %v", productURL, err)
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

func (p *ProductCenter) parseProductLinks(listingURL string) ([]string, error) {
	doc, err := p.fetch(listingURL)
	if err != nil {
		return nil, err
	}
	var links []string
	doc.Find("div.card_item.product a[href^='/products/']").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}
		if strings.Contains(href, "/r-") || strings.Contains(href, "/c-") || strings.Contains(href, "/catalog-") {
			return
		}
		links = append(links, p.absURL(href))
	})
	return links, nil
}

func (p *ProductCenter) parseProductCard(productURL string) (models.PriceItem, error) {
	item := models.PriceItem{URL: productURL}
	doc, err := p.fetch(productURL)
	if err != nil {
		return item, err
	}

	item.Name = strings.TrimSpace(doc.Find("h1").First().Text())
	if item.Name == "" {
		item.Name = strings.TrimSpace(doc.Find("meta[itemprop='name']").First().AttrOr("content", ""))
	}

	doc.Find("div.item_price b").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text != "" {
			item.Price = text
			return
		}
	})

	if item.Price == "" {
		doc.Find("meta[itemprop='price']").Each(func(_ int, s *goquery.Selection) {
			if content, ok := s.Attr("content"); ok && content != "" {
				item.Price = content
				return
			}
		})
	}

	item.Desc = strings.TrimSpace(doc.Find("div.item_descriptor").First().Text())
	if item.Desc == "" {
		item.Desc = strings.TrimSpace(doc.Find("meta[itemprop='description']").First().AttrOr("content", ""))
	}

	item.Category = parseCategory(doc)
	return item, nil
}

func (p *ProductCenter) fetch(rawURL string) (*goquery.Document, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, rawURL)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

func (p *ProductCenter) absURL(href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	b, _ := url.Parse(baseURL)
	r, _ := url.Parse(href)
	return b.ResolveReference(r).String()
}

func parseCategory(doc *goquery.Document) string {
	var cats []string
	doc.Find("ul.crumbs_list li a[itemprop='item']").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("meta[itemprop='name']").First().AttrOr("content", ""))
		if name == "" {
			name = strings.TrimSpace(s.Text())
		}
		href, _ := s.Attr("href")
		if name != "" && (strings.Contains(href, "/catalog-") || strings.Contains(href, "/producers/")) {
			cats = append(cats, name)
		}
	})
	return strings.Join(cats, " / ")
}

func isPriceListFile(href, title string) bool {
	lower := strings.ToLower(href + " " + title)
	for _, kw := range []string{"прайс", "price", "prays", "list", "каталог", "catalog"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	for _, ext := range []string{".xlsx", ".xls", ".csv", ".pdf"} {
		if strings.HasSuffix(strings.ToLower(href), ext) {
			return true
		}
	}
	return false
}

