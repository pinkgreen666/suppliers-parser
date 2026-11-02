package productcenter

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseEmail(t *testing.T) {
	for _, tt := range []struct{ html, want string }{
		{`<a href="mailto:info@norsy.ru?cc=info@productcenter.ru">Написать</a>`, "info@norsy.ru"},
		{`<a href="MAILTO:sales%2Bshop@example.ru?subject=test">Email</a>`, "sales+shop@example.ru"},
		{`<a href="mailto:a@example.ru,b@example.ru?cc=x@example.ru">Email</a><a href="mailto:a@example.ru">a</a>`, "a@example.ru; b@example.ru"},
		{`first@example.ru; second@example.ru`, "first@example.ru; second@example.ru"},
		{`Показать email`, ""},
	} {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader("<div>" + tt.html + "</div>"))
		if err != nil {
			t.Fatal(err)
		}
		if got := parseEmail(doc.Find("div")); got != tt.want {
			t.Errorf("%s: got %q want %q", tt.html, got, tt.want)
		}
	}
}
