ALTER TABLE entitlements DROP CONSTRAINT entitlements_status_check;
ALTER TABLE entitlements ADD CONSTRAINT entitlements_status_check CHECK (status IN ('open', 'claimed', 'issued', 'revoked'));
ALTER TABLE entitlements ADD COLUMN claimed_at timestamptz;

DROP INDEX entitlements_open_by_email;
CREATE INDEX entitlements_unissued_by_email ON entitlements (email) WHERE status IN ('open', 'claimed');
