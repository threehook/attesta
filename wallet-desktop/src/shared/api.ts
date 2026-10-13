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
  | {
      id: string;
      kind: "presentation";
      verifier: string;
      /** The application asking (the origin it answers to), when the request names one the user can trust. */
      application?: string;
      requested: Array<{ type: string[]; claims: string[] }>;
      /** Whether the request only asks who the user is, which the wallet treats as a sign-in. */
      signIn: boolean;
      /** For a sign-in, the identity (email and issuer) that would answer it. */
      identity?: Identity;
      satisfiable: boolean;
    };

// A presentation request the wallet answered by itself, because the user chose to always share with, or sign in to, this application.
export type Shared = { kind: "shared"; verifier: string; claims: string[]; status: number };

// What reading a link gives: something for the user to decide on, or an answer already sent.
export type Prepared = Pending | Shared;

/** Who the user is to an application: the email in a credential, and the DID of the issuer that vouches for it. */
export interface Identity {
  email: string;
  issuer: string;
}

/** An identity the user signs in with automatically, to one application. */
export interface SignInChoice extends Identity {
  /** The application (origin) it applies to. */
  application: string;
}

export interface Settings {
  /** Applications (origins) whose requests are answered without asking. */
  trustedApplications: string[];
  /** Sign-ins that are answered without asking. */
  signIns: SignInChoice[];
}

/** The only claim a sign-in asks for; the backend adds it to every request, so a request with nothing else asks who the user is and nothing more. */
const IDENTITY_CLAIM = "email";

export function isSignIn(requested: Array<{ claims: string[] }>): boolean {
  const claims = requested.flatMap((r) => r.claims);
  return claims.length > 0 && claims.every((claim) => claim === IDENTITY_CLAIM);
}

export type Approved = { kind: "offer"; credentials: HeldCredential[] } | { kind: "presentation"; status: number };

export type Result<T> = { ok: true; value: T } | { ok: false; error: string };

export interface WalletApi {
  list(): Promise<Result<HeldCredential[]>>;
  // Reads a credential offer or a presentation request without acting on it.
  prepare(link: string): Promise<Result<Prepared>>;
  // Carries out what the user agreed to. With remember, a presentation also makes its application one that is answered without asking: for a
  // sign-in that identity signs in to it automatically, for other requests the application gets what it asks for.
  approve(id: string, remember?: boolean): Promise<Result<Approved>>;
  decline(id: string): Promise<Result<null>>;
  settings(): Promise<Result<Settings>>;
  forgetApplication(application: string): Promise<Result<Settings>>;
  forgetSignIn(choice: SignInChoice): Promise<Result<Settings>>;
  // Called with what the main process made of a link the operating system handed to the app (openid-credential-offer:// and openid4vp://).
  onLink(callback: (result: Result<Pending>) => void): void;
  // Called when the settings changed outside the window, such as by choosing "Always share" in a popup.
  onSettingsChanged(callback: () => void): void;
}

export const channels = {
  list: "wallet:list",
  prepare: "wallet:prepare",
  approve: "wallet:approve",
  decline: "wallet:decline",
  settings: "wallet:settings",
  forgetApplication: "wallet:forget-application",
  forgetSignIn: "wallet:forget-sign-in",
  link: "wallet:link",
  settingsChanged: "wallet:settings-changed",
} as const;
