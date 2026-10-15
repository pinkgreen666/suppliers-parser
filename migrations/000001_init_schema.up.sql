CREATE TABLE IF NOT EXISTS company (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source TEXT NOT NULL,
  url TEXT NOT NULL,
  name TEXT,
  official_name TEXT,
  inn TEXT,
  ogrn TEXT,
  address TEXT,
  phone TEXT,
  email TEXT,
  site TEXT,
  telegram TEXT,
  vk TEXT,
  is_active INTEGER NOT NULL DEFAULT 1,
  created TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
  updated TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
  UNIQUE (source, url)
);

CREATE INDEX IF NOT EXISTS idx_company_inn ON company (inn);

CREATE INDEX IF NOT EXISTS idx_company_ogrn ON company (ogrn);

CREATE INDEX IF NOT EXISTS idx_company_source ON company (source);

CREATE INDEX IF NOT EXISTS idx_company_active ON company (is_active);

CREATE TRIGGER IF NOT EXISTS trg_company_updated AFTER
UPDATE ON company FOR EACH ROW WHEN NEW.updated = OLD.updated BEGIN
UPDATE company
SET
  updated = datetime('now', 'localtime')
WHERE
  id = NEW.id;

END;

CREATE TABLE IF NOT EXISTS price_list (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  company_id INTEGER NOT NULL,
  kind TEXT, -- 'file' | 'inline' | NULL 
  url TEXT,
  file_path TEXT,
  items_json TEXT,
  is_active INTEGER NOT NULL DEFAULT 1,
  created TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
  updated TEXT NOT NULL DEFAULT (datetime('now', 'localtime')),
  FOREIGN KEY (company_id) REFERENCES company (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_price_list_company ON price_list (company_id);

CREATE INDEX IF NOT EXISTS idx_price_list_kind ON price_list (kind);

CREATE TRIGGER IF NOT EXISTS trg_price_list_updated AFTER
UPDATE ON price_list FOR EACH ROW WHEN NEW.updated = OLD.updated BEGIN
UPDATE price_list
SET
  updated = datetime('now', 'localtime')
WHERE
  id = NEW.id;

END;
