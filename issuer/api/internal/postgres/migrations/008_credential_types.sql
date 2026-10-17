-- The credential types staff define besides Employee: each has a form of its own. The type and path are fixed when it is created; a type nobody
-- holds can change its fields. A deleted type keeps its row, and its name may be used again.
CREATE TABLE credential_types (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  type        text NOT NULL,
  path        text NOT NULL,
  title       text NOT NULL,
  description text NOT NULL DEFAULT '',
  fields      jsonb NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  deleted_at  timestamptz
);
CREATE UNIQUE INDEX credential_types_type_alive ON credential_types (type) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX credential_types_path_alive ON credential_types (path) WHERE deleted_at IS NULL;

INSERT INTO credential_types (type, path, title, description, fields) VALUES (
  'Diploma Laadpalen Management', 'laadpalen-management', 'Diploma Laadpalen Management', 'Bewijs dat de opleiding Laadpalen Management is afgerond',
  '[{"key":"name","label":"Naam","kind":"text","required":true},{"key":"email","label":"E-mailadres","kind":"email","required":true},{"key":"validUntil","label":"Geldig tot","kind":"date","required":true}]'
);
