package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestExportEnrichedColumns(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	paths, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO company(is_russian,id,source,url,name,inn,category_normal_json) VALUES(1,1,'yopt','https://example.test/c','Company','7719402047','["Ноутбуки"]'); INSERT INTO company_leadership(company_id,status,attempted_at,result_json) VALUES(1,'found',1,'{"inn":"7719402047","leaders":[{"name":"Иванов Иван Иванович","post":"ДИРЕКТОР"}]}');INSERT INTO price_list(company_id,kind,url) VALUES(1,'file','https://example.test/p.pdf');`); err != nil {
		t.Fatal(err)
	}
	f := excelize.NewFile()
	defer f.Close()
	if err := writeCompanies(f, db, ""); err != nil {
		t.Fatal(err)
	}
	if err := writePriceLists(f, db, ""); err != nil {
		t.Fatal(err)
	}
	for _, sheet := range []string{"Компании", "Прайс-листы"} {
		rows, err := f.GetRows(sheet)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range rows[0] {
			for _, bad := range []string{"ID", "Создано", "Обновлено", "Создан", "Обновлён", "Активен", "Телеграм", "ВК"} {
				if h == bad {
					t.Errorf("unexpected %s in %s", h, sheet)
				}
			}
		}
	}
	for cell, want := range map[string]string{"G2": "Ноутбуки", "H2": "Иванов Иван Иванович", "I2": "ДИРЕКТОР"} {
		got, err := f.GetCellValue("Компании", cell)
		if err != nil || got != want {
			t.Errorf("%s=%q want %q: %v", cell, got, want, err)
		}
	}
	for sheet, cell := range map[string]string{"Компании": "O2", "Прайс-листы": "D2"} {
		ok, _, err := f.GetCellHyperLink(sheet, cell)
		if err != nil || !ok {
			t.Errorf("missing link %s %s: %v", sheet, cell, err)
		}
	}
}

func TestOfficialNamePriority(t *testing.T) {
	for _, tt := range []struct {
		name     string
		official any
		result   any
		want     string
	}{
		{"dadata first", "Из карточки", `{"inn":"7719402047","company_name":"ООО Из DaData"}`, "ООО Из DaData"},
		{"no response", "Из карточки", nil, "Из карточки"},
		{"empty dadata name", "Из карточки", `{"inn":"7719402047","company_name":"  "}`, "Из карточки"},
		{"neither name", nil, nil, ""},
		{"different company", "Из карточки", `{"inn":"7707083893","company_name":"Чужая компания"}`, "Из карточки"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			paths, err := filepath.Glob("../../migrations/*.up.sql")
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range paths {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec(string(raw)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := db.Exec(`INSERT INTO company(is_russian,id,source,url,name,inn,official_name) VALUES(1,1,'yopt','https://example.test','Не использовать как официальное','7719402047',?)`, tt.official); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO company_leadership(company_id,status,attempted_at,result_json) VALUES(1,'found',1,?)`, tt.result); err != nil {
				t.Fatal(err)
			}
			f := excelize.NewFile()
			defer f.Close()
			if err := writeCompanies(f, db, ""); err != nil {
				t.Fatal(err)
			}
			got, err := f.GetCellValue("Компании", "C2")
			if err != nil || got != tt.want {
				t.Fatalf("got %q want %q: %v", got, tt.want, err)
			}
		})
	}
}

func TestExportFiltersAllSheetsByRussianCompany(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	paths, err := filepath.Glob("../../migrations/*.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	for i, value := range []any{1, 0, nil} {
		id := i + 1
		if _, err := db.Exec(`INSERT INTO company(id,source,url,name,is_russian) VALUES(?,'yopt',?, 'Company',?)`, id, fmt.Sprint(id), value); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO price_list(company_id,kind,url,items_json) VALUES(?,'inline',?,'[{"name":"Ноутбук"}]')`, id, fmt.Sprint(id)); err != nil {
			t.Fatal(err)
		}
	}
	f := excelize.NewFile()
	defer f.Close()
	for _, write := range []func(*excelize.File, *sql.DB, string) error{writeCompanies, writePriceLists, writePriceItems} {
		if err := write(f, db, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, sheet := range []string{"Компании", "Прайс-листы", "Товары"} {
		rows, err := f.GetRows(sheet)
		if err != nil || len(rows) != 2 {
			t.Fatalf("%s rows=%d err=%v", sheet, len(rows), err)
		}
	}
}
