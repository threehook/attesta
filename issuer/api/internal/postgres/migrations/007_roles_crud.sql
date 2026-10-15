-- A role gets an id, so its name can change, and is deleted softly: a deleted role keeps its row, and its name may be used again.
ALTER TABLE roles DROP CONSTRAINT roles_pkey;
ALTER TABLE roles ADD COLUMN id uuid PRIMARY KEY DEFAULT gen_random_uuid();
ALTER TABLE roles ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE roles ADD COLUMN deleted_at timestamptz;
CREATE UNIQUE INDEX roles_name_alive ON roles (name) WHERE deleted_at IS NULL;
