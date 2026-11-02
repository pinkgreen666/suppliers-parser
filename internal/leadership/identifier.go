package leadership

import (
	"fmt"
	"strconv"
	"strings"
)

type Identifier struct{ Kind, Value, EntityType string }

func (i Identifier) Key() string { return i.Kind + ":" + i.Value }
func digits(s string) bool {
	if s == "" {
		return false
	}
	nonzero := false
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
		nonzero = nonzero || c != '0'
	}
	return nonzero
}
func ValidINN(s string) bool {
	if !digits(s) {
		return false
	}
	sum := func(weights []int) int {
		v := 0
		for i, w := range weights {
			v += w * int(s[i]-'0')
		}
		return v % 11 % 10
	}
	switch len(s) {
	case 10:
		return sum([]int{2, 4, 10, 3, 5, 9, 4, 6, 8}) == int(s[9]-'0')
	case 12:
		return sum([]int{7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) == int(s[10]-'0') && sum([]int{3, 7, 2, 4, 10, 3, 5, 9, 4, 6, 8}) == int(s[11]-'0')
	}
	return false
}
func ValidOGRN(s string) bool {
	if !digits(s) || (len(s) != 13 && len(s) != 15) {
		return false
	}
	n, err := strconv.ParseUint(s[:len(s)-1], 10, 64)
	if err != nil {
		return false
	}
	divisor := uint64(11)
	if len(s) == 15 {
		divisor = 13
	}
	return n%divisor%10 == uint64(s[len(s)-1]-'0')
}
func Identify(c Company) (Identifier, error) {
	inn, ogrn := strings.TrimSpace(c.INN), strings.TrimSpace(c.OGRN)
	if ValidINN(inn) {
		kind := "LEGAL"
		if len(inn) == 12 {
			kind = "INDIVIDUAL"
		}
		return Identifier{"inn", inn, kind}, nil
	}
	if ValidOGRN(ogrn) {
		kind := "LEGAL"
		if len(ogrn) == 15 {
			kind = "INDIVIDUAL"
		}
		return Identifier{"ogrn", ogrn, kind}, nil
	}
	return Identifier{}, fmt.Errorf("нет корректного ИНН или ОГРН; поиск по названию отключён")
}
func MatchesCompany(c Company, r Result) bool {
	inn, ogrn := strings.TrimSpace(c.INN), strings.TrimSpace(c.OGRN)
	return (!ValidINN(inn) || r.INN == inn) && (!ValidOGRN(ogrn) || r.OGRN == ogrn)
}
