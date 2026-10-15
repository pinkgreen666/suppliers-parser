package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"suppliers-parser/internal/models"
	"suppliers-parser/internal/storage"

	"github.com/PuerkitoBio/goquery"
)

const url = "https://yopt.org/postavshiki/category/oborudovanie-instrument/region/rossiya"

var (
	reINN  = regexp.MustCompile(`(\d{10,12})`)
	reOGRN = regexp.MustCompile(`(\d{13,15})`)
)

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

	return c
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
			if _, err := storage.Save(db, company); err != nil {
				log.Printf("Ошибка сохранения: %v", err)
			}
		})
	}
}
