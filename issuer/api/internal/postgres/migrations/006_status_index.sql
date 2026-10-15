-- Every credential gets a position in the status list when its entitlement is first claimed; revoking sets that bit.
CREATE SEQUENCE status_index_seq MINVALUE 0 START 0;
ALTER TABLE entitlements ADD COLUMN status_index bigint UNIQUE;
