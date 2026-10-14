yopt-parser/
├── go.mod
├── cmd/
│   ├── parse/main.go          # CLI: parse --source=yopt
│   └── export/main.go         # CLI: export --out=file.xlsx
├── internal/
│   ├── models/
│   │   └── company.go         # общая структура Company
│   ├── storage/
│   │   └── sqlite.go          # работа с БД (не зависит от сайта)
│   ├── core/
│   │   ├── scraper.go         # общий движок обхода
│   │   └── source.go          # интерфейс Source
│   └── sources/
│       ├── registry.go        # реестр источников
│       ├── yopt/
│       │   └── yopt.go        # адаптер для yopt.org
│       ├── tiu/
│       │   └── tiu.go         # (потом) адаптер для tiu.ru
│       └── supplierx/
│           └── supplierx.go   # (потом) ещё один
└── data.db
