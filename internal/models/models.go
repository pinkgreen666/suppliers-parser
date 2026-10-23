package models

type Company struct {
	Source       string
	URL          string
	Name         string
	OfficialName string
	INN          string
	OGRN         string
	Category     string
	Address      string
	Phone        string
	Email        string
	Site         string
	Telegram     string
	VK           string
	Youtube      string
	PriceLists   []PriceList
}

type PriceList struct {
	CompanyID int64
	Kind      string
	Title     string
	URL       string
	FilePath  string
	Items     []PriceItem
}

type PriceItem struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Price    string `json:"price"`
	Desc     string `json:"desc"`
	Category string `json:"category"`
}
