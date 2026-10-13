import { randomUUID } from "node:crypto";
import {
  acceptPreviewed,
  listCredentials,
  previewCredential,
  readQrImage,
  resolvePresentationRequest,
  submitPresentation,
  type CredentialPreview,
  type PresentationRequest,
  type WalletAgent,
} from "@attesta/wallet";
import { isSignIn, type Approved, type HeldCredential, type Identity, type Pending, type Prepared, type Settings, type SignInChoice } from "../shared/api.js";
import { applicationOf, type WalletSettings } from "./settings.js";

const CREDENTIAL_SCHEME = "openid-credential-offer://";
const PRESENTATION_SCHEME = "openid4vp://";
const PENDING_LIFETIME_MS = 10 * 60 * 1000;
/** An image bigger than this is not a QR code someone mailed. */
const MAX_IMAGE_BYTES = 10 * 1024 * 1024;

export class WalletError extends Error {}


type Waiting = { expires: number } & (
  | { kind: "credential"; credential: CredentialPreview }
  | { kind: "presentation"; request: PresentationRequest; identity?: Identity }
);

// WalletService is what the user can do with the wallet: look at what it holds, and read, then approve or decline, a credential to add or a
// presentation request. Reading never changes anything, except that a presentation request from an application the user chose to share with
// is answered at once; approve is the other way anything changes.

export class WalletService {
  private readonly waiting = new Map<string, Waiting>();

  constructor(
    private readonly agent: WalletAgent,
    private readonly settings: WalletSettings,
    private readonly now: () => number = Date.now,
  ) {}

  list(): Promise<HeldCredential[]> {
    return listCredentials(this.agent);
  }

  /** Reads the QR code in an image the user chose; it holds a link, which is then read like any other. */
  async prepareImage(image: unknown): Promise<Prepared> {
    if (!(image instanceof Uint8Array) || image.byteLength === 0 || image.byteLength > MAX_IMAGE_BYTES) {
      throw new WalletError("Dit is geen afbeelding met een QR-code.");
    }
    let link: string;
    try {
      link = readQrImage(image);
    } catch (error) {
      throw new WalletError(error instanceof Error ? error.message : String(error));
    }
    return this.prepare(link);
  }

  async prepare(link: string): Promise<Prepared> {
    const trimmed = link.trim();
    this.dropExpired();
    const id = randomUUID();
    const expires = this.now() + PENDING_LIFETIME_MS;

    if (trimmed.startsWith(CREDENTIAL_SCHEME)) {
      const credential = await previewCredential(this.agent, trimmed);
      this.waiting.set(id, { kind: "credential", credential, expires });
      return { id, kind: "credential", issuer: credential.issuer, types: credential.types };
    }
    if (trimmed.startsWith(PRESENTATION_SCHEME)) {
      const request = await resolvePresentationRequest(this.agent, trimmed);
      const application = applicationOf(request.verifier);
      const signIn = isSignIn(request.requested);
      const identity = signIn ? await this.identityFor(request) : undefined;
      if (request.satisfiable && (signIn ? this.settings.signsIn(application, identity) : this.settings.trusts(application))) {
        const { status } = await submitPresentation(this.agent, request);
        return { kind: "shared", verifier: request.verifier, claims: [...new Set(request.requested.flatMap((r) => r.claims))], status };
      }
      this.waiting.set(id, { kind: "presentation", request, identity, expires });
      return { id, kind: "presentation", verifier: request.verifier, application, requested: request.requested, signIn, identity, satisfiable: request.satisfiable };
    }
    throw new WalletError("Dit is geen credential-link of presentatieverzoek.");
  }

  async approve(id: string, remember = false): Promise<Approved> {
    const item = this.take(id);
    if (item.kind === "credential") {
      try {
        return { kind: "credential", credentials: await acceptPreviewed(this.agent, item.credential) };
      } catch (error) {
        if (refusalOf(error) === "credential_request_denied") throw new WalletError("Er staat geen credential voor u klaar bij deze uitgever.");
        throw error;
      }
    }
    const { status } = await submitPresentation(this.agent, item.request);
    const application = applicationOf(item.request.verifier);
    if (remember && application) {
      if (!isSignIn(item.request.requested)) this.settings.trust(application);
      else if (item.identity) this.settings.rememberSignIn(application, item.identity);
    }
    return { kind: "presentation", status };
  }

  settingsView(): Settings {
    return this.settings.view();
  }

  forgetApplication(application: string): Settings {
    this.settings.forget(application);
    return this.settings.view();
  }

  /** Whether this identity signs in to this application without asking. */
  signsIn(application: string | undefined, identity: Identity): boolean {
    return this.settings.signsIn(application, identity);
  }

  forgetSignIn(choice: SignInChoice): Settings {
    this.settings.forgetSignIn(choice);
    return this.settings.view();
  }

  /** The identities the wallet holds credentials for (an email, and the issuer vouching for it), of one credential type when given. */
  async identities(type?: string): Promise<Identity[]> {
    const found = new Map<string, Identity>();
    for (const credential of await this.list()) {
      const email = credential.claims.email;
      if (typeof email !== "string" || email === "" || (type !== undefined && credential.type !== type)) continue;
      found.set(`${email}\n${credential.issuer}`, { email, issuer: credential.issuer });
    }
    return [...found.values()];
  }

  // The identity that would answer a sign-in: the one the link named, or else the first held credential of the type asked for.
  private async identityFor(request: PresentationRequest): Promise<Identity | undefined> {
    if (request.hint) return request.hint;
    const types = request.requested.flatMap((r) => r.type);
    for (const type of types.length > 0 ? types : [undefined]) {
      const [first] = await this.identities(type);
      if (first) return first;
    }
    return undefined;
  }

  decline(id: string): void {
    this.waiting.delete(id);
  }

  private take(id: string): Waiting {
    const item = this.waiting.get(id);
    this.waiting.delete(id);
    if (!item || item.expires < this.now()) {
      throw new WalletError("Dit verzoek is verlopen. Open de link opnieuw.");
    }
    return item;
  }

  private dropExpired(): void {
    for (const [id, item] of this.waiting) {
      if (item.expires < this.now()) this.waiting.delete(id);
    }
  }
}

// The error the issuer answered a credential request with, when it was one.
function refusalOf(error: unknown): string | undefined {
  const data = (error as { response?: { credentialErrorResponseResult?: { data?: { error?: unknown } } } })?.response?.credentialErrorResponseResult?.data;
  return typeof data?.error === "string" ? data.error : undefined;
}
