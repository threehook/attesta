import { readFileSync, writeFileSync } from "node:fs";
import type { Identity, Settings, SignInChoice } from "../shared/api.js";

// The application a presentation request comes from: the origin of the address the wallet answers to. Only that is stable between requests; the
// rest of the address names one request. Anything that is not a plain web address (other client id schemes) is no application the user can trust.
export function applicationOf(verifier: string): string | undefined {
  const prefix = "redirect_uri:";
  if (!verifier.startsWith(prefix)) return undefined;
  try {
    const url = new URL(verifier.slice(prefix.length));
    return url.protocol === "https:" || url.protocol === "http:" ? url.origin : undefined;
  } catch {
    return undefined;
  }
}

// What the user chose to let applications do without asking, kept in a file in the wallet's data folder: the applications whose requests are answered
// and the identities the user signs in with automatically. A missing or damaged file means asking every time.
export class WalletSettings {
  private data: Settings;

  constructor(private readonly path: string) {
    this.data = read(path);
  }

  view(): Settings {
    return { trustedApplications: [...this.data.trustedApplications], signIns: this.data.signIns.map((s) => ({ ...s })) };
  }

  /** Whether a request from this application may be answered without asking. */
  trusts(application: string | undefined): boolean {
    return application !== undefined && this.data.trustedApplications.includes(application);
  }

  trust(application: string): void {
    if (this.trusts(application)) return;
    this.data.trustedApplications.push(application);
    this.save();
  }

  forget(application: string): void {
    this.data.trustedApplications = this.data.trustedApplications.filter((a) => a !== application);
    this.save();
  }

  /** Whether this identity signs in to this application without asking. */
  signsIn(application: string | undefined, identity: Identity | undefined): boolean {
    return application !== undefined && identity !== undefined && this.data.signIns.some((s) => same(s, application, identity));
  }

  rememberSignIn(application: string, identity: Identity): void {
    if (this.signsIn(application, identity)) return;
    this.data.signIns.push({ application, email: identity.email, issuer: identity.issuer });
    this.save();
  }

  forgetSignIn(choice: SignInChoice): void {
    this.data.signIns = this.data.signIns.filter((s) => !same(s, choice.application, choice));
    this.save();
  }

  private save(): void {
    writeFileSync(this.path, JSON.stringify(this.data, null, 2), { mode: 0o600 });
  }
}

const same = (s: SignInChoice, application: string, identity: Identity) =>
  s.application === application && s.email === identity.email && s.issuer === identity.issuer;

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((a): a is string => typeof a === "string") : [];
}

function signIns(value: unknown): SignInChoice[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((v): SignInChoice[] => {
    const c = v as Partial<SignInChoice> | null;
    return c && typeof c.application === "string" && typeof c.email === "string" && typeof c.issuer === "string"
      ? [{ application: c.application, email: c.email, issuer: c.issuer }]
      : [];
  });
}

function read(path: string): Settings {
  try {
    const parsed = JSON.parse(readFileSync(path, "utf8")) as Partial<Settings>;
    return { trustedApplications: strings(parsed.trustedApplications), signIns: signIns(parsed.signIns) };
  } catch {
    return { trustedApplications: [], signIns: [] };
  }
}
