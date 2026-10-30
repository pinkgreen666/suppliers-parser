package productcenter

import (
	"net/mail"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func parseEmail(cell *goquery.Selection) string {
	var candidates []string
	cell.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
		href, _ := a.Attr("href")
		if len(href) >= 7 && strings.EqualFold(href[:7], "mailto:") {
			address := strings.SplitN(href[7:], "?", 2)[0]
			if decoded, err := url.PathUnescape(address); err == nil {
				address = decoded
			}
			candidates = append(candidates, address)
		}
	})
	if len(candidates) == 0 {
		candidates = append(candidates, cell.Text())
	}
	seen := map[string]bool{}
	var result []string
	for _, candidate := range candidates {
		for _, address := range strings.FieldsFunc(candidate, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
			address = strings.TrimSpace(address)
			parsed, err := mail.ParseAddress(address)
			if err != nil {
				continue
			}
			key := strings.ToLower(parsed.Address)
			if !seen[key] {
				seen[key] = true
				result = append(result, parsed.Address)
			}
		}
	}
	return strings.Join(result, "; ")
}
