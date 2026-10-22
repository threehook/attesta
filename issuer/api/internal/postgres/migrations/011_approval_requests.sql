-- An action of a workload that no mandate covers waits here until people approve it. The workload then takes one Approval credential for it.
CREATE TABLE approval_requests (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workload     text NOT NULL,
  resource     text NOT NULL,
  action       jsonb NOT NULL DEFAULT '{}',
  created_at   timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL,
  rejected_at  timestamptz,
  delivered_at timestamptz
);
-- A workload has one request at a time for the same resource.
CREATE UNIQUE INDEX approval_requests_one_open ON approval_requests (workload, resource) WHERE rejected_at IS NULL AND delivered_at IS NULL;

CREATE TABLE approval_request_approvals (
  request_id  uuid NOT NULL REFERENCES approval_requests (id),
  approver    text NOT NULL,
  approved_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (request_id, approver)
);
