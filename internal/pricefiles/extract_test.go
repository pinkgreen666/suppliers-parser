package pricefiles

import (
	"context"
	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkbookSheetsHeadersAndSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "price.xlsx")
	f := excelize.NewFile()
	for _, sheet := range []string{"Sheet1", "Other"} {
		if sheet != "Sheet1" {
			f.NewSheet(sheet)
		}
		for i, row := range [][]any{{"Прайс-лист"}, {"Артикул", "Наименование товара", "Цена"}, {"A1", "Ноутбук Lenovo", 50000}, {"", "Компьютеры"}, {"", "Итого", 50000}, {"A2", "Монитор", 10000}} {
			cell, _ := excelize.CoordinatesToCellName(1, i+1)
			if err := f.SetSheetRow(sheet, cell, &row); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	f.Close()
	r, err := Extract(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "extracted" || len(r.Products) != 4 {
		t.Fatalf("%+v", r)
	}
	if p := r.Products[0]; p.Name != "Ноутбук Lenovo" || p.SourceSheet != "Sheet1" || p.SourceRow != 3 || p.SourceFile != path {
		t.Fatalf("%+v", p)
	}
}
func TestCSVEncodingsDelimitersAndQuotes(t *testing.T) {
	for _, sep := range []string{";", ",", "\t"} {
		for _, cp1251 := range []bool{false, true} {
			text := "Артикул" + sep + "Наименование" + sep + "Цена\n1" + sep + "\"Ноутбук, модель; A\"" + sep + "100\n"
			b := []byte(text)
			if cp1251 {
				var err error
				b, err = charmap.Windows1251.NewEncoder().Bytes(b)
				if err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(t.TempDir(), "price.csv")
			os.WriteFile(path, b, 0600)
			r, err := Extract(context.Background(), path)
			if err != nil || len(r.Products) != 1 || r.Products[0].Name != "Ноутбук, модель; A" {
				t.Fatalf("sep=%q cp1251=%t: %+v %v", sep, cp1251, r, err)
			}
		}
	}
}
func TestPDFTableAndProse(t *testing.T) {
	text := "Сертификат: ноутбуки и мониторы\n\fАртикул  Наименование           Цена\nA1       Ноутбук Lenovo         1000\nA2       Монитор                500\n\n         Итого                  1500\n"
	p := pdfTables(text)
	if len(p) != 2 || p[0].Name != "Ноутбук Lenovo" || p[0].SourcePage != 2 || p[0].SourceRow != 2 {
		t.Fatalf("%+v", p)
	}
	if len(pdfTables("Компания производит ноутбуки. Телефон 12345")) != 0 {
		t.Fatal("prose treated as products")
	}
}
func TestUnsupportedBrokenAndCancellation(t *testing.T) {
	for _, ext := range []string{".docx", ".xlsx"} {
		path := filepath.Join(t.TempDir(), "file"+ext)
		os.WriteFile(path, []byte("broken"), 0600)
		r, err := Extract(context.Background(), path)
		if ext == ".docx" && (err != nil || r.Status != "unsupported") {
			t.Fatalf("%+v %v", r, err)
		}
		if ext == ".xlsx" && err == nil {
			t.Fatal("corrupt workbook accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "price.csv")
	os.WriteFile(path, []byte("Наименование;Цена\nНоутбук;100"), 0600)
	if _, err := Extract(ctx, path); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestPDFWithoutTextSkipsOCR(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "pdftotext")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '\\f'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(dir, "scan.pdf")
	os.WriteFile(path, []byte("%PDF-test"), 0600)
	r, err := Extract(context.Background(), path)
	if err != nil || r.Status != "no_text" || !strings.Contains(r.Message, "OCR") {
		t.Fatalf("%+v %v", r, err)
	}
}
