ALTER TABLE company ADD COLUMN is_russian INTEGER CHECK (is_russian IN (0,1));
