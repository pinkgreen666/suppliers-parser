package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"suppliers-parser/internal/models"
	"suppliers-parser/internal/storage"

	"github.com/PuerkitoBio/goquery"
)

const (
	baseURL     = "https://yopt.org"
	url         = "https://yopt.org/postavshiki/category/oborudovanie-instrument/region/rossiya"
	sourceName  = "yopt"
	maxPages    = 5
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

func parseCard(url string) models.Company {
	c := models.Company{URL: url}
	req, _ := http.NewRequest("GET", url, nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		log.Fatal(err)
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
				// println(value)
			}
		} else {
			spans := s.Find("span")
			if spans.Length() > 1 {
				value = strings.TrimSpace(spans.Eq(1).Text())
			}
		}
		value = strings.TrimSpace(value)
		// fmt.Println(label)
		// fmt.Println(value)

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

	pl, err := parsePriceList(doc)
	print(pl)

	return c
}

func parsePriceList(doc *goquery.Document) (*models.PriceList, error) {
	// Try to find inline price table first
	var priceItems []models.PriceItem
	var priceTitle string

	// Look for price tables with class indicating it's a price list
	doc.Find("div.tovarypostavshika0-each").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Find("div.title-stroka .title a").Text())
		if name == "" {
			return
		}
		href, _ := s.Find("div.title-stroka .title a").Attr("href")
		desc := strings.TrimSpace(s.Find("div.text-obj").Text())
		price := strings.TrimSpace(s.Find("span.costs").Text())
		price = strings.TrimSpace(strings.TrimPrefix(price, "Цена"))

		items = append(items, models.PriceItem{
			Name:  name,
			URL:   href,
			Price: price,
			Desc:  desc,
		})
	})

	var fileURLs []string
	doc.Find("div.prikrfiles a").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || href == "" {
			return
		}
		// title := strings.TrimSpace(a.Text())

		fileURLs = append(fileURLs, href)
	})

	// Prepare price list data
	pl := &models.PriceList{}

	if len(priceItems) > 0 {
		// Create inline price list
		itemsJSON, err := json.Marshal(priceItems)
		if err != nil {
			return nil, fmt.Errorf("marshal price items: %w", err)
		}
		pl.Kind = "inline"
		pl.Title = priceTitle
		pl.ItemsJSON = string(itemsJSON)
		return pl, nil
	}

	if len(fileURLs) > 0 {
		// Create file-based price list (first file)
		pl.Kind = "file"
		pl.Title = "Прайс-лист (вложение)"
		pl.URL = fileURLs[0]
		// FilePath would be set during download processing
		return pl, nil
	}

	return nil, nil // No price list found
}

func main() {
	db, err := storage.Open("data.db")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	for i := range 5 {
		page := fmt.Sprintf("?page=%d", i)
		url_page := url + page

		req, _ := http.NewRequest("GET", url_page, nil)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()

		doc, err := goquery.NewDocumentFromReader(resp.Body)
		if err != nil {
			log.Fatal(err)
		}

		// fmt.Println("Title:", doc.Find("title").Text())
		// fmt.Println("Кол-во field-content:", doc.Find("div.field-content").Length())

		doc.Find("div.field-content").Each(func(i int, s *goquery.Selection) {
			name := s.Find("div.title-stroka h2").Text()
			href, _ := s.Find("a.kartzagl").Attr("href")
			fmt.Printf("%d, %s -> %s\n", i+1, name, href)
			company := parseCard(href)
			// fmt.Println(company)
			companyID, err := storage.Save(db, company)
			if err != nil {
				log.Printf("Ошибка сохранения компании: %v", err)
				// continue
			}

			// Parse price list from company page
			priceList, err := parsePriceList(company)
			if err != nil {
				log.Printf("Ошибка парсинга прайс-листа: %v", err)
			} else if priceList != nil {
				priceList.CompanyID = companyID
				_, err = storage.SavePriceList(db, *priceList)
				if err != nil {
					log.Printf("Ошибка сохранения прайс-листа: %v", err)
				}
			}
		})
	}
}
