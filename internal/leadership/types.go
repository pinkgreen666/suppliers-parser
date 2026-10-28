package leadership

import (
	"context"
	"time"
)

type Company struct {
	ID     int64  `json:"id"`
	Source string `json:"source"`
	Name   string `json:"name"`
	INN    string `json:"inn"`
	OGRN   string `json:"ogrn"`
}
type Leader struct {
	Name string `json:"name"`
	Type string `json:"type"` // PERSON, LEGAL, INDIVIDUAL, UNKNOWN
	Post string `json:"post,omitempty"`
	INN  string `json:"inn,omitempty"`
	OGRN string `json:"ogrn,omitempty"`
}
type Result struct {
	Status            string     `json:"status"`
	Note              string     `json:"note,omitempty"`
	INN               string     `json:"inn,omitempty"`
	OGRN              string     `json:"ogrn,omitempty"`
	EntityType        string     `json:"entity_type,omitempty"`
	CompanyName       string     `json:"company_name,omitempty"`
	Leaders           []Leader   `json:"leaders"`
	Source            string     `json:"source"`
	CheckedAt         time.Time  `json:"checked_at"`
	RegistryUpdatedAt *time.Time `json:"registry_updated_at,omitempty"`
}
type Record struct {
	Company     Company   `json:"company"`
	Key         string    `json:"lookup_key"`
	Status      string    `json:"status"`
	Error       string    `json:"error,omitempty"`
	AttemptedAt time.Time `json:"attempted_at"`
	Result      *Result   `json:"result,omitempty"`
	Cached      bool      `json:"cached"`
}
type Cache struct {
	Result      *Result
	AttemptedAt time.Time
	Error       string
}
type Provider interface {
	Lookup(context.Context, Identifier) (Result, error)
}
type Store interface {
	GetCache(context.Context, string) (Cache, error)
	PutCache(context.Context, string, Cache) error
	SaveRecord(context.Context, Record) (Record, error)
}
