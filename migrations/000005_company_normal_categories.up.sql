ALTER TABLE company ADD COLUMN category_normal_json TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(category_normal_json) AND json_type(category_normal_json) = 'array');
