-- What named people allow a workload to do. A mandate becomes a Mandate credential each time the workload asks for its credentials, so it is bound to the
-- key the workload's wallet has then. The resource is a glob that the policy matches against the resource asked about.
CREATE TABLE mandates (
  id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workload   text NOT NULL,
  resource   text NOT NULL,
  expires_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  ended_at   timestamptz
);
-- A wallet holds one credential per type, so a workload has one mandate at a time.
CREATE UNIQUE INDEX mandates_one_per_workload ON mandates (workload) WHERE ended_at IS NULL;

CREATE TABLE mandate_approvals (
  mandate_id  uuid NOT NULL REFERENCES mandates (id),
  approver    text NOT NULL,
  approved_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (mandate_id, approver)
);
