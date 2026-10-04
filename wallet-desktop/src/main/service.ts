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
import type { Approved, HeldCredential, Pending } from "../shared/api.js";

const OFFER_SCHEME = "openid-credential-offer://";
const PRESENTATION_SCHEME = "openid4vp://";
const PENDING_LIFETIME_MS = 10 * 60 * 1000;

export class WalletError extends Error {}

type Waiting = { expires: number } & ({ kind: "offer"; offer: CredentialOffer } | { kind: "presentation"; request: PresentationRequest });

// WalletService is what the user can do with the wallet: look at what it holds, and read, then approve or decline, a credential offer or a
// presentation request. Reading never changes anything; only approve does.
export class WalletService {
  private readonly waiting = new Map<string, Waiting>();

  constructor(
    private readonly agent: WalletAgent,
    private readonly now: () => number = Date.now,
  ) {}

  list(): Promise<HeldCredential[]> {
    return listCredentials(this.agent);
  }

  async prepare(link: string): Promise<Pending> {
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
      this.waiting.set(id, { kind: "presentation", request, expires });
      return { id, kind: "presentation", verifier: request.verifier, requested: request.requested, satisfiable: request.satisfiable };
    }
    throw new WalletError("This is not a credential offer or a presentation request.");
  }

  async approve(id: string): Promise<Approved> {
    const item = this.take(id);
    if (item.kind === "offer") {
      return { kind: "offer", credentials: await acceptPreviewedOffer(this.agent, item.offer) };
    }
    return { kind: "presentation", status: (await submitPresentation(this.agent, item.request)).status };
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
