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

// ExistingCompanyFilter lets a source skip detail pages using a stable URL
// discovered in its listing before doing the expensive company parse.
type ExistingCompanyFilter interface {
	ParseWithFilter(ctx context.Context, shouldSkip func(source, url string) (bool, error)) (<-chan models.Company, error)
}

type Stats struct {
	SavedCompanies int
	SavedPrices    int
	Skipped        int
	Failed         int
}
