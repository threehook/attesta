-- The roles staff can give an employee. A name has no comma, because an employee's roles are kept as one text separated by commas.
CREATE TABLE roles (
  name       text PRIMARY KEY CHECK (name <> '' AND name = btrim(name) AND position(',' IN name) = 0),
  created_at timestamptz NOT NULL DEFAULT now()
);

INSERT INTO roles (name) VALUES ('laadpalen-aanvrager');
