-- An entitlement belongs to a person (email) or to a workload (its SPIFFE ID), never both.
ALTER TABLE entitlements ALTER COLUMN email DROP NOT NULL;
ALTER TABLE entitlements ADD COLUMN workload text;
ALTER TABLE entitlements ADD CONSTRAINT entitlements_one_holder CHECK ((email IS NULL) <> (workload IS NULL));
CREATE UNIQUE INDEX entitlements_one_unissued_workload ON entitlements (workload, type, claims) WHERE workload IS NOT NULL AND status IN ('open', 'claimed');
CREATE INDEX entitlements_by_workload ON entitlements (workload) WHERE workload IS NOT NULL;

-- The systems that get a credential of their own. A revoked one keeps its row, and its SPIFFE ID may be enrolled again.
CREATE TABLE workloads (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  spiffe_id   text NOT NULL,
  owner       text NOT NULL,
  environment text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  revoked_at  timestamptz
);
CREATE UNIQUE INDEX workloads_spiffe_id_enrolled ON workloads (spiffe_id) WHERE revoked_at IS NULL;
