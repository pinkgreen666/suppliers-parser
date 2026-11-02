package categorize

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExcelSelectionAndSafeJoin(t *testing.T) {
	f := excelize.NewFile()
	defer f.Close()
	f.SetSheetName("Sheet1", "Компании")
	f.NewSheet("Товары")
	companies := [][]any{
		{"ID", "Название", "ИНН"},
		{"1", "Одна", "00123"},
		{"2", "Без товаров", ""},
		{"3", "Дубль", "00999"},
		{"4", "Четвёртая", "4"},
		{"5", "Пятая", "5"},
		{"6", "Дубль", "00999"},
	}
	products := [][]any{
		{"Компания", "ИНН", "Название товара"},
		{"Одна", "00123", "Пылесос"},
		{"Дубль", "00999", "Ноутбук"},
		{"Одна", "99999", "Чужой товар"},
	}
	for _, sheet := range []struct {
		name string
		rows [][]any
	}{{"Компании", companies}, {"Товары", products}} {
		for i, row := range sheet.rows {
			cell, _ := excelize.CoordinatesToCellName(1, i+1)
			if err := f.SetSheetRow(sheet.name, cell, &row); err != nil {
				t.Fatal(err)
			}
		}
	}
	path := filepath.Join(t.TempDir(), "input.xlsx")
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	got, err := (ExcelSource{Path: path}).Load(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 || got[4].ID != "5" {
		t.Fatalf("wrong selection: %+v", got)
	}
	if got[0].INN != "00123" || len(got[0].Products) != 1 || got[0].Products[0].Row != 2 {
		t.Fatalf("wrong join: %+v", got[0])
	}
	if len(got[1].Products) != 0 {
		t.Fatal("invented products")
	}
	if len(got[2].Warnings) == 0 || len(got[2].Products) != 0 {
		t.Fatal("ambiguous company joined")
	}
	if _, err := (ExcelSource{Path: path}).Load(context.Background(), 0); err == nil {
		t.Fatal("invalid limit accepted")
	}
}
