CREATE TABLE price_file_extraction (
 price_list_id INTEGER PRIMARY KEY REFERENCES price_list(id) ON DELETE CASCADE,
 fingerprint TEXT NOT NULL,
 parser_version INTEGER NOT NULL,
 products_json TEXT NOT NULL,
 status TEXT NOT NULL,
 message TEXT NOT NULL DEFAULT ''
);
