package storage

import (
	"context"
	"suppliers-parser/internal/models"
	"testing"
)

func TestGeographyPersistenceAndInvalidation(t *testing.T) {
	db := leadershipDB(t)
	ctx := context.Background()
	id, err := db.SaveCompany(models.Company{Source: "yopt", URL: "geo", Address: "Казахстан, Алматы"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClassifyGeography(ctx); err != nil {
		t.Fatal(err)
	}
	var value *int
	if err := db.sql.QueryRow(`SELECT is_russian FROM company WHERE id=?`, id).Scan(&value); err != nil || value == nil || *value != 0 {
		t.Fatalf("%v %v", value, err)
	}
	if _, err := db.SaveCompany(models.Company{Source: "yopt", URL: "geo", Address: "Казахстан, Алматы", Name: "Changed"}); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRow(`SELECT is_russian FROM company WHERE id=?`, id).Scan(&value); err != nil || value == nil || *value != 0 {
		t.Fatalf("same evidence lost classification: %v %v", value, err)
	}
	if _, err := db.SaveCompany(models.Company{Source: "yopt", URL: "geo", Address: "Россия, Москва"}); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRow(`SELECT is_russian FROM company WHERE id=?`, id).Scan(&value); err != nil || value != nil {
		t.Fatalf("stale classification: %v %v", value, err)
	}
	if _, err := db.ClassifyGeography(ctx); err != nil {
		t.Fatal(err)
	}
	if err := db.sql.QueryRow(`SELECT is_russian FROM company WHERE id=?`, id).Scan(&value); err != nil || value == nil || *value != 1 {
		t.Fatalf("%v %v", value, err)
	}
}
