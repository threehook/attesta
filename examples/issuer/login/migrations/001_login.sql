CREATE TABLE login_requests (
  id               uuid PRIMARY KEY,
  client_id        text NOT NULL,
  redirect_uri     text NOT NULL,
  state            text,
  code_challenge   text NOT NULL,
  scope            text,
  nonce            text,
  status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'mailed', 'confirmed', 'used')),
  email            text,
  mails_sent       integer NOT NULL DEFAULT 0,
  token_hash       text,
  token_expires_at timestamptz,
  code_hash        text,
  code_expires_at  timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  expires_at       timestamptz NOT NULL
);

CREATE UNIQUE INDEX login_requests_token ON login_requests (token_hash) WHERE token_hash IS NOT NULL;
CREATE UNIQUE INDEX login_requests_code ON login_requests (code_hash) WHERE code_hash IS NOT NULL;

CREATE TABLE login_mails (
  email   text NOT NULL,
  sent_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX login_mails_by_email ON login_mails (email, sent_at);
