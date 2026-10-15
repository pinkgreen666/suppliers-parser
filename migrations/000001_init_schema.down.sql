DROP TRIGGER IF EXISTS trg_price_list_updated;

DROP TRIGGER IF EXISTS trg_company_updated;

DROP INDEX IF EXISTS idx_price_list_kind;

DROP INDEX IF EXISTS idx_price_list_company;

DROP TABLE IF EXISTS price_list;

DROP INDEX IF EXISTS idx_company_active;

DROP INDEX IF EXISTS idx_company_source;

DROP INDEX IF EXISTS idx_company_ogrn;

DROP INDEX IF EXISTS idx_company_inn;

DROP TABLE IF EXISTS company;
