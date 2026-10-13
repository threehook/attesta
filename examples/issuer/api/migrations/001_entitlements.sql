CREATE TABLE entitlements (
  id          uuid PRIMARY KEY,
  email       text NOT NULL,
  type        text NOT NULL,
  claims      jsonb NOT NULL,
  valid_until timestamptz,
  status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'issued', 'revoked')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  issued_at   timestamptz
);

CREATE INDEX entitlements_open_by_email ON entitlements (email) WHERE status = 'open';
