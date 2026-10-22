package models

type PriceList struct {
	CompanyID int64
	Kind      string
	Title     string
	URL       string
	FilePath  string
	ItemsJSON string
}

type PriceItem struct {
	Name     string
	URL      string
	Price    string
	Desc     string
	Category string
}
