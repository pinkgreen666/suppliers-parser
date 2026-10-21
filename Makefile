# Путь к папке с миграциями
MIGRATIONS_PATH = migrations
# Строка подключения (для SQLite с драйвером sqlite3)
DB_URL = sqlite3://./data.db
# Имя бинарника migrate (если он не в PATH, укажите полный путь)
MIGRATE = migrate

.PHONY: help migrate-version migrate-up migrate-up-one migrate-down migrate-down-one migrate-create migrate-force

help: ## Показать доступные команды
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

migrate-version: ## Показать текущую версию миграций
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DB_URL)" version

migrate-up: ## Применить все ожидающие миграции
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DB_URL)" up

migrate-up-one: ## Применить только одну следующую миграцию
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DB_URL)" up 1

migrate-down: ## Откатить последнюю миграцию
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DB_URL)" down 1

migrate-down-all: ## Откатить все миграции (осторожно!)
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DB_URL)" down -all

migrate-create: ## Создать новую миграцию (make migrate-create name=add_something)
	$(MIGRATE) create -ext sql -dir $(MIGRATIONS_PATH) -seq $(name)

migrate-force: ## Принудительно установить версию (make migrate-force version=1)
	$(MIGRATE) -path $(MIGRATIONS_PATH) -database "$(DB_URL)" force $(version)
