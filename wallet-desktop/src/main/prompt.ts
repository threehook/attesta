import type { Pending } from "../shared/api.js";

type PresentationRequest = Extract<Pending, { kind: "presentation" }>;

export const SHARE = 0;
export const ALWAYS = 1;

export interface Prompt {
  message: string;
  detail: string;
  /** What the popup offers; the user's answer is the index. The last one declines. */
  buttons: string[];
  /** Whether the user can make this automatic for the application, which needs an application the user can trust. */
  canRemember: boolean;
  /** For a sign-in, remembering is a checkbox next to the buttons; otherwise it is the "Altijd delen" button. */
  checkboxLabel?: string;
  signIn: boolean;
}

// The popup for a presentation request that has to be asked about: a sign-in, or a request for more than who the user is. The window stays closed for these; only credential offers open it.
export function promptFor(request: PresentationRequest): Prompt {
  return request.signIn ? signInPrompt(request) : sharePrompt(request);
}

// The popup for a sign-in: who the user would be, and whether to sign in without the application's button from now on.
export function signInPrompt(request: PresentationRequest): Prompt {
  const who = request.application ?? request.verifier;
  const canRemember = request.application !== undefined && request.identity !== undefined;
  return {
    message: `Aanmelden bij ${who}?`,
    detail: request.identity ? `E-mailadres: ${request.identity.email}\nDID: ${request.identity.issuer}` : "",
    buttons: ["Bevestigen", "Annuleren"],
    canRemember,
    checkboxLabel: canRemember ? "Altijd aanmelden, zonder de knop Aanmelden" : undefined,
    signIn: true,
  };
}

export function sharePrompt(request: PresentationRequest): Prompt {
  const who = request.application ?? request.verifier;
  const lines = request.requested.flatMap((r) => ["wie het heeft uitgegeven", ...r.claims]);
  const canRemember = request.application !== undefined;
  return {
    message: `Informatie delen met ${who}?`,
    detail: [...new Set(lines)].map((line) => `• ${line}`).join("\n"),
    buttons: canRemember ? ["Delen", "Altijd delen", "Weigeren"] : ["Delen", "Weigeren"],
    canRemember,
    signIn: false,
  };
}

// What to say when the wallet holds nothing that answers the request.
export function nothingToShare(request: PresentationRequest): { message: string; detail: string } {
  return { message: `${request.application ?? request.verifier} vraagt om iets wat u niet hebt`, detail: "U hebt geen credential dat op dit verzoek past." };
}

// What an answer to the popup means; checked is the state of its checkbox, if it has one.
export function decision(prompt: Prompt, response: number, checked = false): "share" | "always" | "decline" {
  if (prompt.signIn) return response !== SHARE ? "decline" : checked && prompt.canRemember ? "always" : "share";
  if (response === SHARE) return "share";
  if (prompt.canRemember && response === ALWAYS) return "always";
  return "decline";
}
