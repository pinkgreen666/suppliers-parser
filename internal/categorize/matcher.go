package categorize

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode"
)

// Aliases are explicit equivalences, not a similarity score. Every phrase
// must identify the product itself; descriptions may mention unrelated goods.
type Alias struct {
	Category string   `json:"category"`
	Phrases  []string `json:"phrases"`
	Exclude  []string `json:"exclude,omitempty"`
	Enabled  *bool    `json:"enabled,omitempty"`
	Kind     string   `json:"kind,omitempty"`
	Note     string   `json:"note,omitempty"`
}
type RuleFile struct {
	Aliases []Alias `json:"aliases"`
}

func LoadAliases(path string) ([]Alias, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules RuleFile
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, err
	}
	return rules.Aliases, nil
}

type Match struct {
	Category Category `json:"category"`
	Method   string   `json:"method"`
	Evidence string   `json:"evidence"`
}
type ProductResult struct {
	Product Product `json:"product"`
	Status  string  `json:"status"`
	Matches []Match `json:"matches"`
	Reason  string  `json:"reason,omitempty"`
}
type Assignment struct {
	Category    Category `json:"category"`
	ProductRows []int    `json:"product_rows"`
}
type CompanyResult struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	INN        string          `json:"inn,omitempty"`
	Status     string          `json:"status"`
	Categories []Assignment    `json:"categories"`
	Products   []ProductResult `json:"products"`
	Warnings   []string        `json:"warnings,omitempty"`
}

type compiledRule struct {
	category Category
	phrase   string
	terms    []string
	exclude  [][]string
}
type Matcher struct {
	rules []compiledRule
	index map[string][]int
}

func NewMatcher(categories []Category, rules []Alias) (*Matcher, error) {
	m := &Matcher{index: map[string][]int{}}
	byName := map[string]Category{}
	byID := map[int][]Alias{}
	for _, c := range categories {
		byName[normalize(c.Name)] = c
	}
	for _, r := range rules {
		c, ok := byName[normalize(r.Category)]
		if !ok {
			return nil, fmt.Errorf("категория правила %q отсутствует в справочнике", r.Category)
		}
		if r.Enabled != nil && !*r.Enabled {
			if r.Note == "" {
				return nil, fmt.Errorf("для отключённой категории %q нужна причина", r.Category)
			}
		} else if len(r.Phrases) == 0 {
			return nil, fmt.Errorf("пустое правило %q", r.Category)
		}
		for _, p := range r.Phrases {
			if normalize(p) == "" {
				return nil, fmt.Errorf("пустой синоним %q", r.Category)
			}
		}
		byID[c.ID] = append(byID[c.ID], r)
	}
	for _, c := range categories {
		rs := byID[c.ID]
		if len(rs) == 0 {
			rs = []Alias{{Category: c.Name, Phrases: []string{c.Name}}}
		}
		for _, r := range rs {
			if r.Enabled != nil && !*r.Enabled {
				continue
			}
			var exclude [][]string
			for _, word := range r.Exclude {
				exclude = append(exclude, tokens(word))
			}
			for _, phrase := range r.Phrases {
				terms := tokens(phrase)
				rule := compiledRule{category: c, phrase: phrase, terms: terms, exclude: exclude}
				m.index[terms[0]] = append(m.index[terms[0]], len(m.rules))
				m.rules = append(m.rules, rule)
			}
		}
	}
	return m, nil
}

func normalize(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "ё", "е")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }), " ")
}
func stem(word string) string {
	// Preserve noun identities that would collide when removing a soft sign.
	switch word {
	case "уголь", "угля", "углем", "углю", "угли":
		return "уголь"
	case "медь", "меди", "медью":
		return "медь"
	}
	// Deliberately small inflection normalizer; does not claim full morphology.
	for _, suffix := range []string{"иями", "ями", "ами", "ого", "его", "ому", "ему", "ыми", "ими", "иях", "ах", "ях", "ов", "ев", "ий", "ый", "ой", "ая", "яя", "ое", "ее", "ые", "ие", "ых", "их", "ом", "ем", "ы", "и", "а", "я", "у", "ю", "е", "ь"} {
		if strings.HasSuffix(word, suffix) {
			root := strings.TrimSuffix(word, suffix)
			if len([]rune(root)) >= 4 {
				return root
			}
		}
	}
	return word
}
func tokens(s string) []string {
	ts := strings.Fields(normalize(s))
	for i := range ts {
		ts[i] = stem(ts[i])
	}
	return ts
}

func phraseMatch(title, phrase string) bool { return matchTerms(tokens(title), tokens(phrase)) }

func containsTerms(title, phrase []string) bool {
	if len(phrase) == 0 {
		return false
	}
	for i := 0; i+len(phrase) <= len(title); i++ {
		same := true
		for j := range phrase {
			if title[i+j] != phrase[j] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}

// Require the phrase to identify the item, not a material, accessory or use.
func matchTerms(title, phrase []string) bool {
	if len(phrase) == 0 {
		return false
	}
	for start := 0; start < len(title); start++ {
		blocked := false
		for _, word := range title[:start] {
			switch word {
			case "для", "к", "от", "без", "с", "со", "из", "под", "в", "на":
				blocked = true
			}
		}
		if blocked {
			continue
		}
		// A phrase after an unrelated noun is not the subject of the title.
		if start > 0 {
			for _, word := range title[:start] {
				latin := true
				for _, r := range word {
					if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
						latin = false
						break
					}
				}
				modifier := strings.HasSuffix(word, "н") || strings.HasSuffix(word, "ск") || strings.HasSuffix(word, "ов") || strings.HasSuffix(word, "ев") || strings.HasSuffix(word, "ющ") || strings.HasSuffix(word, "яч") || word == "нов" || word == "сухой" || word == "мини"
				if !latin && !modifier {
					blocked = true
					break
				}
			}
			if blocked {
				continue
			}
		}
		// Multiword labels may have adjective/noun inversion and up to two
		// intervening modifiers. A full word set is required, never a partial one.
		maxEnd := start + len(phrase)
		if len(phrase) > 1 {
			maxEnd += 2
		}
		if maxEnd > len(title) {
			maxEnd = len(title)
		}
		for end := start + len(phrase); end <= maxEnd; end++ {
			foreignRelation := false
			for _, word := range title[start:end] {
				switch word {
				case "для", "к", "от", "без", "с", "со", "из", "под", "в", "на":
					if !containsToken(phrase, word) {
						foreignRelation = true
					}
				}
			}
			if foreignRelation {
				continue
			}
			remaining := map[string]int{}
			for _, word := range phrase {
				remaining[word]++
			}
			for _, word := range title[start:end] {
				if remaining[word] > 0 {
					remaining[word]--
				}
			}
			complete := true
			for _, n := range remaining {
				if n > 0 {
					complete = false
					break
				}
			}
			if complete && containsToken(phrase, title[start]) {
				return true
			}
		}
	}
	return false
}
func containsToken(terms []string, word string) bool {
	for _, t := range terms {
		if t == word {
			return true
		}
	}
	return false
}

func (m *Matcher) ClassifyProduct(p Product) ProductResult {
	result := ProductResult{Product: p, Status: "unmatched", Matches: []Match{}}
	title := tokens(p.Name)
	candidates := map[int]bool{}
	for _, word := range title {
		for _, i := range m.index[word] {
			candidates[i] = true
		}
	}
	ordered := make([]int, 0, len(candidates))
	for i := range candidates {
		ordered = append(ordered, i)
	}
	sort.Ints(ordered)
	matched := map[int]bool{}
	for _, i := range ordered {
		rule := m.rules[i]
		if matched[rule.category.ID] {
			continue
		}
		excluded := false
		for _, phrase := range rule.exclude {
			if containsTerms(title, phrase) {
				excluded = true
				break
			}
		}
		if excluded || !matchTerms(title, rule.terms) {
			continue
		}
		method := "alias"
		if normalize(rule.phrase) == normalize(rule.category.Name) {
			method = "category_name"
		}
		result.Matches = append(result.Matches, Match{rule.category, method, rule.phrase})
		matched[rule.category.ID] = true
	}
	var kept []Match
	for i, a := range result.Matches {
		broader := false
		for j, b := range result.Matches {
			if i != j && len(tokens(b.Category.Name)) > len(tokens(a.Category.Name)) && categoryContains(b.Category.Name, a.Category.Name) {
				broader = true
				break
			}
		}
		if !broader {
			kept = append(kept, a)
		}
	}
	if len(kept) > 0 {
		result.Matches = kept
		result.Status = "matched"
	} else {
		result.Reason = "Нет подтверждённого соответствия названию или словарю; требуется проверка. Это не доказывает отсутствие категории в справочнике."
	}
	return result
}

func (m *Matcher) Classify(c Company) CompanyResult {
	result := CompanyResult{ID: c.ID, Name: c.Name, INN: c.INN, Status: "no_matches", Categories: []Assignment{}, Products: []ProductResult{}, Warnings: c.Warnings}
	byCategory := map[int]*Assignment{}
	for _, p := range c.Products {
		r := m.ClassifyProduct(p)
		result.Products = append(result.Products, r)
		for _, match := range r.Matches {
			if byCategory[match.Category.ID] == nil {
				byCategory[match.Category.ID] = &Assignment{Category: match.Category}
			}
			a := byCategory[match.Category.ID]
			a.ProductRows = append(a.ProductRows, p.Row)
		}
	}
	for _, a := range byCategory {
		result.Categories = append(result.Categories, *a)
	}
	sort.Slice(result.Categories, func(i, j int) bool { return result.Categories[i].Category.ID < result.Categories[j].Category.ID })
	if len(c.Products) == 0 {
		result.Status = "insufficient_data"
	} else if len(result.Categories) > 0 {
		result.Status = "matched"
		for _, p := range result.Products {
			if p.Status != "matched" {
				result.Status = "partial"
				break
			}
		}
	}
	return result
}

// Taxonomy containment does not apply product-title subject restrictions.
func categoryContains(specific, broad string) bool {
	available := map[string]int{}
	for _, word := range tokens(specific) {
		available[word]++
	}
	for _, word := range tokens(broad) {
		if available[word] == 0 {
			return false
		}
		available[word]--
	}
	return true
}
