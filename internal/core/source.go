package core

import (
	"context"
	"net/http"

	"suppliers-parser/internal/models"
)

type Source interface {
	Name() string
	HTTPClient() *http.Client
	Parse(ctx context.Context) (<-chan models.Company, error)
}

type Stats struct {
	SavedCompanies int
	SavedPrices    int
	Skipped        int
	Failed         int
}
