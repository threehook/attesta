CREATE TABLE oid4vci_sessions (
  id                          uuid PRIMARY KEY,
  offer_id                    text NOT NULL UNIQUE,
  entitlement_id              uuid NOT NULL REFERENCES entitlements (id),
  credential_configuration_id text NOT NULL,
  code_hash                   text NOT NULL UNIQUE,
  state                       text NOT NULL DEFAULT 'offer' CHECK (state IN ('offer', 'token', 'completed')),
  expires_at                  timestamptz NOT NULL,
  access_token_hash           text UNIQUE,
  access_token_expires_at     timestamptz,
  created_at                  timestamptz NOT NULL,
  completed_at                timestamptz
);

CREATE INDEX oid4vci_sessions_by_entitlement ON oid4vci_sessions (entitlement_id, created_at);
