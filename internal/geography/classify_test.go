package geography

import (
	"suppliers-parser/internal/leadership"
	"testing"
)

func TestClassify(t *testing.T) {
	valid := leadership.Result{Source: "dadata", Status: "found", EntityType: "LEGAL", INN: "7719402047"}
	for _, tt := range []struct {
		name string
		e    Evidence
		want int
	}{
		{"registry", Evidence{INN: "7719402047", Registry: &valid}, 1},
		{"pair", Evidence{INN: "7719402047", OGRN: "1157746078984"}, 1},
		{"one identifier", Evidence{INN: "7719402047"}, -1},
		{"Russia", Evidence{Address: "Россия, г. Москва"}, 1},
		{"foreign", Evidence{Address: "Казахстан, Алматы"}, 0},
		{"conflict", Evidence{INN: "7719402047", Registry: &valid, Address: "Беларусь, Минск"}, -1},
		{"no evidence", Evidence{}, -1},
		{"phone or ambiguous city not sufficient", Evidence{Address: "Москва"}, -1},
		{"wrong company", Evidence{INN: "7707083893", Registry: &valid}, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := Classify(tt.e)
			got := -1
			if r.IsRussian != nil {
				got = 0
				if *r.IsRussian {
					got = 1
				}
			}
			if got != tt.want || r.Reason == "" {
				t.Fatalf("%+v want %d", r, tt.want)
			}
		})
	}
}
