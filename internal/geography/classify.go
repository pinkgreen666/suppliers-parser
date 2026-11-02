// Package geography classifies company jurisdiction from locally stored evidence.
package geography

import (
	"regexp"
	"strings"

	"suppliers-parser/internal/leadership"
)

type Evidence struct {
	INN      string
	OGRN     string
	Address  string
	Registry *leadership.Result
}
type Decision struct {
	IsRussian *bool  `json:"is_russian"`
	Reason    string `json:"reason"`
}

func decision(value bool, reason string) Decision { return Decision{&value, reason} }

var russian = regexp.MustCompile(`(?i)(^|[^\pL])(россия|российская федерация|рф)([^\pL]|$)`)
var foreign = regexp.MustCompile(`(?i)(^|[^\pL])(беларусь|белоруссия|казахстан|украина|китай|киргизия|кыргызстан|узбекистан|таджикистан|армения|азербайджан|грузия|турция|польша|германия|литва|латвия|эстония|молдова|молдавия|оаэ|индия|вьетнам)([^\pL]|$)`)

// Address recognition deliberately requires a country name; ambiguous cities are not evidence.
func Classify(e Evidence) Decision {
	inn, ogrn := strings.TrimSpace(e.INN), strings.TrimSpace(e.OGRN)
	ru, other := russian.MatchString(e.Address), foreign.MatchString(e.Address)
	c := leadership.Company{INN: inn, OGRN: ogrn}
	_, idErr := leadership.Identify(c)
	registry := e.Registry != nil && e.Registry.Source == "dadata" && idErr == nil && leadership.MatchesCompany(c, *e.Registry) && (e.Registry.Status == "found" || e.Registry.Status == "no_management") && (e.Registry.EntityType == "LEGAL" || e.Registry.EntityType == "INDIVIDUAL")
	strong := registry || (leadership.ValidINN(inn) && leadership.ValidOGRN(ogrn) && ((len(inn) == 10 && len(ogrn) == 13) || (len(inn) == 12 && len(ogrn) == 15)))
	if (ru && other) || (other && strong) {
		return Decision{Reason: "Противоречие страны адреса и других признаков; нужна проверка"}
	}
	if registry {
		return decision(true, "Сохранённый ответ DaData совпадает с реквизитами")
	}
	if strong {
		return decision(true, "Корректные российские ИНН и ОГРН согласованного типа (без проверки реестра)")
	}
	if ru {
		return decision(true, "В адресе явно указана Россия")
	}
	if other {
		return decision(false, "В адресе явно указана другая страна")
	}
	return Decision{Reason: "Недостаточно данных: страна в адресе не указана, подтверждения реестра или пары реквизитов нет"}
}
