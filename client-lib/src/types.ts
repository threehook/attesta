// Shared types for talking to the Attesta backend (see backend/internal/httpapi).

// A credential the user's wallet is asked for. The type, the issuer and the holder's email are always established; claims lists what the wallet
// is asked to disclose on top of that.
export interface CredentialRequest {
  credentialType: string;
  claims?: string[];
}

// What an application asks the user's wallet for: one or more credentials of the same person, each of a type of its own, the first identifying
// the person. `credentialType` with `claims` is shorthand for a single credential.
export type AuthorizationRequest = { resource: string; policyId: string } & (
  | { credentials: CredentialRequest[] }
  | ({ credentials?: undefined } & CredentialRequest)
);

export interface AuthorizationRequestResponse {
  requestId: string;
  // An `openid4vp://` link the application hands to the user's wallet.
  authorizationRequest: string;
}

// Who was allowed: the email address the issuer vouches for. Absent for a denial, because an untrusted issuer's claims prove nothing.
export interface Subject {
  issuer: string;
  email: string;
}

export type Decision = "Permit" | "Deny" | "NotApplicable" | "Indeterminate";

export interface AttributeAssignment {
  attributeId: string;
  value: string;
}

// Obligations must be carried out with the decision; advice may be ignored.
export interface Statement {
  id: string;
  attributeAssignments: AttributeAssignment[];
}

export type AuthorizationOutcome =
  | { status: "pending" }
  | { status: "done"; decision: Decision; reason: string; subject?: Subject; obligations?: Statement[]; advice?: Statement[] };

export interface PutPolicyRequest {
  id: string;
  source: string;
}

export interface PutPolicyResponse {
  status: string;
  id: string;
}
