import { randomUUID } from "node:crypto";
import {
  acceptPreviewedOffer,
  listCredentials,
  previewCredentialOffer,
  resolvePresentationRequest,
  submitPresentation,
  type CredentialOffer,
  type PresentationRequest,
  type WalletAgent,
} from "@attesta/wallet";
import { isSignIn, type Approved, type HeldCredential, type Identity, type Pending, type Prepared, type Settings, type SignInChoice } from "../shared/api.js";
import { applicationOf, type WalletSettings } from "./settings.js";

const OFFER_SCHEME = "openid-credential-offer://";
const PRESENTATION_SCHEME = "openid4vp://";
const PENDING_LIFETIME_MS = 10 * 60 * 1000;

export class WalletError extends Error {}


type Waiting = { expires: number } & (
  | { kind: "offer"; offer: CredentialOffer }
  | { kind: "presentation"; request: PresentationRequest; identity?: Identity }
);

// WalletService is what the user can do with the wallet: look at what it holds, and read, then approve or decline, a credential offer or a
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

  async prepare(link: string): Promise<Prepared> {
    const trimmed = link.trim();
    this.dropExpired();
    const id = randomUUID();
    const expires = this.now() + PENDING_LIFETIME_MS;

    if (trimmed.startsWith(OFFER_SCHEME)) {
      const offer = await previewCredentialOffer(this.agent, trimmed);
      this.waiting.set(id, { kind: "offer", offer, expires });
      return { id, kind: "offer", issuer: offer.issuer, types: offer.types };
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
    throw new WalletError("This is not a credential offer or a presentation request.");
  }

  async approve(id: string, remember = false): Promise<Approved> {
    const item = this.take(id);
    if (item.kind === "offer") {
      return { kind: "offer", credentials: await acceptPreviewedOffer(this.agent, item.offer) };
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
      throw new WalletError("This request has expired. Open the link again.");
    }
    return item;
  }

  private dropExpired(): void {
    for (const [id, item] of this.waiting) {
      if (item.expires < this.now()) this.waiting.delete(id);
    }
  }
}
