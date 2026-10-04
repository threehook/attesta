// The contract between the Electron main process and the renderer, shared by both.

export interface HeldCredential {
  id: string;
  type: string;
  issuer: string;
  claims: Record<string, unknown>;
}

// Something that needs the user's decision before the wallet acts on it.
export type Pending =
  | { id: string; kind: "offer"; issuer: string; types: string[] }
  | { id: string; kind: "presentation"; verifier: string; requested: Array<{ type: string[]; claims: string[] }>; satisfiable: boolean };

export type Approved = { kind: "offer"; credentials: HeldCredential[] } | { kind: "presentation"; status: number };

export type Result<T> = { ok: true; value: T } | { ok: false; error: string };

export interface WalletApi {
  list(): Promise<Result<HeldCredential[]>>;
  // Reads a credential offer or a presentation request without acting on it.
  prepare(link: string): Promise<Result<Pending>>;
  // Carries out what the user agreed to.
  approve(id: string): Promise<Result<Approved>>;
  decline(id: string): Promise<Result<null>>;
  // Called with links the operating system hands to the app (openid-credential-offer:// and openid4vp://).
  onLink(callback: (link: string) => void): void;
}

export const channels = {
  list: "wallet:list",
  prepare: "wallet:prepare",
  approve: "wallet:approve",
  decline: "wallet:decline",
  link: "wallet:link",
} as const;
