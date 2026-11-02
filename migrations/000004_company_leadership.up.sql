CREATE TABLE leadership_cache (
    lookup_key TEXT PRIMARY KEY,
    result_json TEXT,
    checked_at INTEGER,
    attempted_at INTEGER NOT NULL,
    last_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE company_leadership (
    company_id INTEGER PRIMARY KEY REFERENCES company(id) ON DELETE CASCADE,
    lookup_key TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    result_json TEXT,
    attempted_at INTEGER NOT NULL,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_company_leadership_lookup ON company_leadership(lookup_key);
