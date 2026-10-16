package yopt

import (
	"encoding/json"
	"fmt"
	"internal/runtime/pprof/label"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"suppliers-parser/internal/core"
	"suppliers-parser/internal/models"

	"github.com/PuerkitoBio/goquery"
)

const {
	baseURL = "https://yopt.org"
	startURL = "https://yopt.org/postavshiki/category/oborudovanie-instrument/region/rossia"
	sourceName = "yopt"
}

var (
	reINN = regexp.MustCompile(`(\d{10,12})`)
	reOGRN = regexp.MustCompile(`(\d{13,15})`)
)

type Yopt struct {
	client *http.Client
}

func New() core.Source {
	return &Yopt{client: http.DefaultClient}
}

func init() {
	core.Register(sourceName, New)
}

func (y *Yopt) Name() string { return sourceName }
func (y *Yopt) StartURL() string { return startURL }

func (y *Yopt) ParseListing(pageURL string) ([]string, string, error) {
	doc, err := fetch(y.client, pageURL)
	if err != nil {
		return nil, "", err
	}

	var cards []string
	doc.Find("div.field-content").Each(func(i int, s *goquery.Selection) {
		if href, ok := s.Find("a.kartzagl").Attr("href"); ok && href != "" {
			cards = append(cards, absURL(href))
		}
	})

	var next string
	doc.Find("li.pager-next a").First().Each(func(i int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			next = absURL(href)
		}
	})
	return cards, next, nil
}

func (y *Yopt) ParseCard(cardURL string) (models.Company, []models.PriceList, error) {
	c := models.Company{Source: sourceName, URL: cardURL}

	doc, err := fetch(y.client, cardURL)
	if err != nil {
		return c, nil, err
	}

	doc.Find("div.listInfo-supplier").Each(func(i int, s *goquery.Selection) {
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
			if m := reOGRN.FindAllStringSubmatch(value); m != nil {
				c.OGRN = m[1]
			}
		}
	})

	doc.Find("div.contn2 > div").Each(func(i int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("span").First().Text())
		var value string
		if a := s.Find("a").First(); a.Length() > 0 {
			value = strings.TrimSpace(a.Text())
			value = strings.TrimSpace(strings.TrimSuffix(value, "Показать"))
			value = strings.TrimSpace(strings.TrimSuffix(value, "Открыть"))
			if value == "" {
				value, _ = a.Attr("href")
			}
		} else {
			spans := s.Find("span")
			if spans.Length() > 1 {
				value = strings.TrimSpace(spans.Eq(1).Text())
			}
		}
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

	if c.OfficialName == "" {
		c.OfficialName = strings.TrimSpace(doc.Find("h1").First().Text())
	}

	prices := parsePriceLists(doc)
	return c, prices, nil
}

func fetch(client *http.Client, rawURL string) (*goquery.Document, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return goquery.NewDocumentFromReader(resp.Body)
}

func absURL(href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	b, _ := url.Parse(baseURL)
	r, _ := url.Parse(href)
	return b.ResolveReference(r).String()
}

func parsePriceLists(doc *goquery.Document) []models.PriceList {
	var result []models.PriceList

	doc.Find("div.prikrfiles a").Each(func(i int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}
		title := strings.TrimSpace(a.Text())
		if !isPriceListFile(href, title) {
			return
		}
		result = append(result, models.PriceList{
			Kind: "file",
			Title: title,
			URL: absURL(href),
		})
	})

	items := parseInlineItems(doc)
	if len(items) > 0 {
		data, _ := json.Marshal(items)
		result = append(result, models.PriceList{
			Kind: "inline",
			Title: "Товары на сайте",
			ItemsJSON: string(data),
		})
	}
	return result
}

func isPriceListFile(href, title string) bool {
	lower := strings.ToLower(href + " " + title)
	for _, kw := range []string{"прайс", "price", "prays", "list"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	for _, ext := range []string{".xls", ".xlsx", ".csv"} {
		if strings.HasSuffix(strings.ToLower(href), ext) {
			return true
		}
	}
	return false
}

func parseInlineItems(doc *goquery.Document) []models.PriceItem {
	var items []models.PriceItem
	doc.Find("div.tovarypostavshika0-each").Each(func(i int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.title-stroka .title a").Text())
		if name == "" {
			return
		}
		href, _ := s.Find("div.title-stroka .title a").Attr("href")
		desc := strings.TrimSpace(s.Find("div.text-obj").Text())
		price := strings.TrimSpace(s.Find("span.costs").Text())
		price = strings.TrimSpace(strings.TrimPrefix(price, "Цена:"))

		items = append(items, models.PriceItem{
			Name: name, URL: absURL(href), Price: price, Desc: desc,
		})
	})

	return items
}
