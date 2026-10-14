-- Older duplicates (same person, type and data, not yet issued) are withdrawn, the oldest stays.
UPDATE entitlements e SET status = 'revoked'
WHERE status IN ('open', 'claimed')
  AND EXISTS (
    SELECT 1 FROM entitlements o
    WHERE o.email = e.email AND o.type = e.type AND o.claims = e.claims AND o.status IN ('open', 'claimed')
      AND (o.created_at, o.id) < (e.created_at, e.id));

CREATE UNIQUE INDEX entitlements_one_unissued ON entitlements (email, type, claims) WHERE status IN ('open', 'claimed');
