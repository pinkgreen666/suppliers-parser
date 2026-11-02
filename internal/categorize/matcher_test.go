package categorize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestConservativeCategoryMatching(t *testing.T) {
	cats := []Category{{ID: 1, Name: "Ноутбуки"}, {ID: 2, Name: "Пылесосы"}, {ID: 3, Name: "Строительные пылесосы"}, {ID: 4, Name: "Дезсредства"}}
	rules := []Alias{{Category: "Ноутбуки", Phrases: []string{"ноутбук", "персональные мобильные компьютеры"}}, {Category: "Дезсредства", Phrases: []string{"дезинфицирующее средство"}}}
	m, err := NewMatcher(cats, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		want int
	}{
		{"Персональный мобильный компьютер X15", 1},
		{"Компьютерная техника", 0},
		{"Сумка для ноутбука", 0},
		{"Мешки для пылесосов", 0},
		{"Строительный пылесос", 3},
		{"Дезинфицирующее средство", 4},
		{"Неизвестный товар", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := m.ClassifyProduct(Product{Name: tc.name, Description: "Также подходит для ноутбуков и пылесосов"})
			if tc.want == 0 {
				if len(r.Matches) != 0 {
					t.Fatalf("unexpected: %+v", r.Matches)
				}
			} else if len(r.Matches) != 1 || r.Matches[0].Category.ID != tc.want {
				t.Fatalf("got %+v, want %d", r.Matches, tc.want)
			}
		})
	}
}

func TestCompanyAggregationAndNoData(t *testing.T) {
	m, _ := NewMatcher([]Category{{ID: 1, Name: "Пылесосы"}}, nil)
	r := m.Classify(Company{Products: []Product{{Row: 2, Name: "Пылесос A"}, {Row: 3, Name: "Пылесос B"}, {Row: 4, Name: "Неизвестный товар"}}})
	if r.Status != "partial" || len(r.Categories) != 1 || len(r.Categories[0].ProductRows) != 2 {
		t.Fatalf("got %+v", r)
	}
	if r := m.Classify(Company{}); r.Status != "insufficient_data" || len(r.Categories) != 0 {
		t.Fatalf("got %+v", r)
	}
}

func TestTaxonomyDuplicatesAndUnknownAlias(t *testing.T) {
	path := filepath.Join(t.TempDir(), "categories")
	if err := os.WriteFile(path, []byte("\ufeffПылесосы\n\nНоутбуки\n пылесосы \n"), 0600); err != nil {
		t.Fatal(err)
	}
	cats, err := LoadCategories(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0].ID != 1 || len(cats[0].Lines) != 2 || cats[1].ID != 3 {
		t.Fatalf("got %+v", cats)
	}
	if _, err := NewMatcher(cats, []Alias{{Category: "Выдуманная категория", Phrases: []string{"x"}}}); err == nil {
		t.Fatal("unknown category accepted")
	}
}

func TestActualAliasesUseTaxonomy(t *testing.T) {
	cats, err := LoadCategories("../../category-normal")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := LoadAliases("../../config/category-aliases.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMatcher(cats, rules)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Дозатор дезинфектанта Ksitex", "Парогенератор для уборки Cleanfix", "Компьютерная техника", "Сумка для ноутбука"} {
		r := m.ClassifyProduct(Product{Name: name})
		for _, match := range r.Matches {
			if match.Category.Name == "Дезсредства" || match.Category.Name == "Ноутбуки" || match.Category.Name == "Парогенераторы и гладильные системы" {
				t.Errorf("wrong match for %s: %+v", name, match)
			}
		}
	}
}

func TestFullDictionaryCoverage(t *testing.T) {
	cats, err := LoadCategories("../../category-normal")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := LoadAliases("../../config/category-aliases.json")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range rules {
		counts[normalize(r.Category)]++
	}
	for _, c := range cats {
		if counts[normalize(c.Name)] != 1 {
			t.Errorf("%s: expected exactly one rule", c.Name)
		}
	}
	if len(rules) != len(cats) {
		t.Errorf("rules=%d categories=%d", len(rules), len(cats))
	}
	if _, err := NewMatcher(cats, rules); err != nil {
		t.Fatal(err)
	}
}

func TestDictionaryRegression(t *testing.T) {
	cats, err := LoadCategories("../../category-normal")
	if err != nil {
		t.Fatal(err)
	}
	rules, err := LoadAliases("../../config/category-aliases.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewMatcher(cats, rules)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/regression.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Title    string   `json:"title"`
		Expected []string `json:"expected"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Title, func(t *testing.T) {
			r := m.ClassifyProduct(Product{Name: tc.Title})
			got := []string{}
			for _, match := range r.Matches {
				got = append(got, match.Category.Name)
			}
			sort.Strings(got)
			sort.Strings(tc.Expected)
			if !reflect.DeepEqual(got, tc.Expected) {
				t.Errorf("got %v; want %v", got, tc.Expected)
			}
		})
	}
}
