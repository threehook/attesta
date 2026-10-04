// Shared types for talking to the zk-puoi backend (see backend/internal/httpapi).

// What an application asks the user's wallet for. The credential type, the issuer and the holder's email are always established; claims lists what
// the wallet is asked to disclose on top of that.
export interface AuthorizationRequest {
  resource: string;
  policyId: string;
  credentialType: string;
  claims?: string[];
}

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

export type AuthorizationOutcome = { status: "pending" } | { status: "done"; allow: boolean; reason: string; subject?: Subject };

export interface PutPolicyRequest {
  id: string;
  source: string;
}

export interface PutPolicyResponse {
  status: string;
  id: string;
}
