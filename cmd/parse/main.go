package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"suppliers-parser/internal/models"
	"suppliers-parser/internal/storage"

	"github.com/PuerkitoBio/goquery"
)

const (
	baseURL     = "https://yopt.org"
	startURL    = "https://yopt.org/postavshiki/category/oborudovanie-instrument/region/rossiya"
	sourceName  = "yopt"
	maxPages    = 10
	httpTimeout = 20 * time.Second
)

var (
	reINN     = regexp.MustCompile(`(\d{10,12})`)
	reOGRN    = regexp.MustCompile(`(\d{13,15})`)
	rePriceID = regexp.MustCompile(`price-\d+`) // for extracting price item IDs
)

var httpClient = &http.Client{
	Timeout: httpTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

func fetch(rawURL string) (*goquery.Document, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d, for %s", resp.StatusCode, rawURL)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}
	return doc, nil
}

func parseCategory(doc *goquery.Document) string {
	var cats []string

	doc.Find(`div[itemtype='https://schema.org/BreadcrumbList'] span[itemprop='itemListElement']`).Each(func(i int, s *goquery.Selection) {
		href, _ := s.Find("a[itemprop='item']").Attr("href")
		name := strings.TrimSpace(s.Find("span[itemprop='name']").Text())
		if name == "" {
			return
		}

		if !strings.Contains(href, "/postavshiki/category/") {
			return
		}

		cats = append(cats, name)
	})

	return strings.Join(cats, " / ")
}

func parseCard(companyURL string) (models.Company, []models.PriceList, error) {
	c := models.Company{
		Source: sourceName,
		URL:    companyURL,
	}

	doc, err := fetch(companyURL)
	if err != nil {
		return c, nil, err
	}

	c.Category = parseCategory(doc)
	fmt.Println(c.Category)

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
			if m := reOGRN.FindStringSubmatch(value); m != nil {
				c.OGRN = m[1]
			}
		}
	})

	doc.Find("div.contn2 > div").Each(func(i int, s *goquery.Selection) {
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
			c.Telegram = value
		case strings.Contains(label, "Вконтакте"):
			c.VK = value
		case strings.Contains(label, "Youtube"):
			c.Youtube = value
		}
	})

	prices := parsePriceList(doc)

	return c, prices, nil
}

func parsePriceList(doc *goquery.Document) []models.PriceList {
	var result []models.PriceList

	doc.Find("div.prikrfiles a").Each(func(i int, a *goquery.Selection) {
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
			URL:   absURL(href),
		})
	})

	items := parseInlineItems(doc)
	if len(items) > 0 {
		href, _ := doc.Find("div.vsetovarypost a").Attr("href")
		href = strings.TrimSpace(href)
		data, err := json.Marshal(items)
		if err == nil {
			result = append(result, models.PriceList{
				Kind:      "inline",
				Title:     "Товары на сайте",
				URL:       absURL(href),
				ItemsJSON: string(data),
			})
		}
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

	for _, ext := range []string{".xlsx", ".xls", ".csv"} {
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
			Name:  name,
			URL:   absURL(href),
			Price: price,
			Desc:  desc,
		})
	})

	return items
}

func absURL(href string) string {
	if strings.HasPrefix(href, "http") {
		return href
	}
	b, _ := url.Parse(baseURL)
	r, _ := url.Parse(href)
	return b.ResolveReference(r).String()
}

func main() {
	force := flag.Bool("force", false, "перепарсить все компании")
	flag.Parse()

	db, err := storage.Open("data.db")
	if err != nil {
		log.Fatalf("открытие БД: %v", err)
	}
	defer db.Close()

	savedCompanies := 0
	savedPrices := 0
	skipped := 0
	failed := 0

	for page := 0; page < maxPages; page++ {
		pageURL := fmt.Sprintf("%s?page=%d", startURL, page)

		doc, err := fetch(pageURL)
		if err != nil {
			log.Printf("ошибка листинга: %v", err)
			continue
		}

		type card struct {
			name string
			href string
		}
		var cards []card

		doc.Find("div.field-content").Each(func(i int, s *goquery.Selection) {
			name := s.Find("div.title-stroka h2").Text()
			href, _ := s.Find("a.kartzagl").Attr("href")
			if href == "" {
				return
			}
			cards = append(cards, card{name: name, href: href})
		})

		for _, crd := range cards {
			exists, err := storage.Exists(db, sourceName, crd.href)
			if err != nil {
				log.Printf("ошибка Exists %s: %v", crd.href, err)
				continue
			}
			if exists && !*force {
				skipped++
				continue
			}

			company, prices, err := parseCard(crd.href)
			if err != nil {
				failed++
				log.Printf("ошибка карточки %s: %v", crd.href, err)
				continue
			}
			company.Name = crd.name

			companyID, err := storage.Save(db, company)
			if err != nil {
				failed++
				log.Printf("ошибка сохранение %s: %v", crd.name, err)
				continue
			}
			savedCompanies++

			for i := range prices {
				prices[i].CompanyID = companyID

				if prices[i].Kind == "file" && prices[i].URL != "" {
					prefix := fmt.Sprintf("%s_%d_", sourceName, companyID)

					path, err := storage.DownloadPriceFile(httpClient, prices[i].URL, prefix)
					if err != nil {
						log.Printf("ошибка скачивания %s, %v", prices[i].URL, err)
					} else {
						prices[i].FilePath = path
					}
				}

				fmt.Printf("%+v\n", prices[i])
				if err := storage.SavePriceList(db, prices[i]); err != nil {
					log.Printf("ошибка прайса %s: %v", crd.name, err)
					continue
				}
				savedPrices++
			}

			time.Sleep(300 * time.Millisecond)
		}
	}

	fmt.Println(savedCompanies)
	fmt.Println(savedPrices)
	fmt.Println(skipped)
	fmt.Println(failed)
}
