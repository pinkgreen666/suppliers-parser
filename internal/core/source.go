package core

import "suppliers-parser/internal/models"

type Source interface {
	Name() string

	StartURL() string

	ParseListing(pageURL string) (cards []string, nextURL string, err error)

	ParseCard(cardURL string) (models.Company, []models.PriceList, error)
}f
