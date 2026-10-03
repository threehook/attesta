// Shared types for talking to the zk-puoi backend (see backend/internal/httpapi) and for building/holding proofs.

// Groth16Proof is snarkjs's native proof.json shape — the exact format backend/internal/proof.SnarkjsVerifier expects in /v1/authorize's "proof"
// field, unmodified.
export interface Groth16Proof {
  pi_a: [string, string, string];
  pi_b: [[string, string], [string, string], [string, string]];
  pi_c: [string, string, string];
  protocol: "groth16";
  curve: string;
}

// PublicSignals is snarkjs's public.json shape: one decimal string per public circuit output, in declaration order.
export type PublicSignals = string[];

export interface LoginRequest {
  subject: string;
  roles?: string[];
}

export interface LoginResponse {
  token: string;
}

export interface AuthorizeRequest {
  resource: string;
  policyId?: string;
  proof: Groth16Proof;
  publicSignals: PublicSignals;
  // issuer is a disclosed credential claim passed separately from publicSignals — see backend/internal/authz.Input's doc comment for why this is a
  // stand-in, not the final design.
  issuer?: string;
}

export interface AuthorizeResponse {
  allow: boolean;
  reason: string;
}

export interface PutPolicyRequest {
  id: string;
  source: string;
}

export interface PutPolicyResponse {
  status: string;
  id: string;
}
