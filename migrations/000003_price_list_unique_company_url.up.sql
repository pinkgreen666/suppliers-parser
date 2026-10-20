DROP INDEX IF EXISTS idx_price_list_company;

DROP INDEX IF EXISTS idx_price_list_kind;

DROP TRIGGER IF EXISTS trg_price_list_updated;

CREATE TABLE
    price_list_new (
        id INTEGER PRIMARY KEY AUTOINCREMENT,
        company_id INTEGER NOT NULL,
        kind TEXT,
        url TEXT,
        file_path TEXT,
        items_json TEXT,
        is_active INTEGER NOT NULL DEFAULT 1,
        created TEXT NOT NULL DEFAULT (datetime ('now', 'localtime')),
        updated TEXT NOT NULL DEFAULT (datetime ('now', 'localtime')),
        FOREIGN KEY (company_id) REFERENCES company (id) ON DELETE CASCADE,
        UNIQUE (company_id, url)
    );

INSERT INTO
    price_list_new (
        id,
        company_id,
        kind,
        url,
        file_path,
        items_json,
        is_active,
        created,
        updated
    )
SELECT
    id,
    company_id,
    kind,
    url,
    file_path,
    items_json,
    is_active,
    created,
    updated
FROM
    price_list
WHERE
    id IN (
        SELECT
            MIN(id)
        FROM
            price_list
        GROUP BY
            company_id,
            url
    );

DROP TABLE price_list;

ALTER TABLE price_list_new
RENAME TO price_list;

CREATE INDEX IF NOT EXISTS idx_price_list_company ON price_list (company_id);

CREATE INDEX IF NOT EXISTS idx_price_list_kind ON price_list (kind);

CREATE TRIGGER IF NOT EXISTS trg_price_list_updated AFTER
UPDATE ON price_list FOR EACH ROW WHEN NEW.updated = OLD.updated BEGIN
UPDATE price_list
SET
    updated = datetime ('now', 'localtime')
WHERE
    id = NEW.id;

END;