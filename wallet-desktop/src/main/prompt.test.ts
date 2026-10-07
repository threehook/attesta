import { describe, expect, it } from "vitest";
import { ALWAYS, decision, nothingToShare, promptFor, sharePrompt, signInPrompt } from "./prompt.js";

const request = (application?: string) => ({
  id: "1",
  kind: "presentation" as const,
  verifier: "redirect_uri:https://api.example/v1/r/response",
  application,
  requested: [{ type: ["Employee"], claims: ["department", "diploma"] }],
  signIn: false,
  satisfiable: true,
});

describe("sharePrompt", () => {
  it("names the application and lists what would be shared, once each", () => {
    const p = sharePrompt({ ...request("https://api.example"), requested: [{ type: ["A"], claims: ["email"] }, { type: ["B"], claims: ["email", "diploma"] }] });
    expect(p.message).toContain("https://api.example");
    expect(p.detail).toBe("• wie het heeft uitgegeven\n• email\n• diploma");
  });

  it("offers Altijd delen only for an application the user can trust", () => {
    expect(sharePrompt(request("https://api.example")).buttons).toEqual(["Delen", "Altijd delen", "Weigeren"]);
    expect(sharePrompt(request(undefined)).buttons).toEqual(["Delen", "Weigeren"]);
  });
});

describe("decision", () => {
  it("reads the answer", () => {
    const withAlways = sharePrompt(request("https://api.example"));
    expect(decision(withAlways, 0)).toBe("share");
    expect(decision(withAlways, ALWAYS)).toBe("always");
    expect(decision(withAlways, 2)).toBe("decline");
  });

  it("never treats the second button as Always when there is none", () => {
    const without = sharePrompt(request(undefined));
    expect(decision(without, 1)).toBe("decline");
    expect(decision(without, 0)).toBe("share");
  });
});

describe("nothingToShare", () => {
  it("says the wallet holds nothing that answers the request", () => {
    expect(nothingToShare(request("https://api.example")).detail).toContain("geen credential");
  });
});

describe("signInPrompt", () => {
  const identity = { email: "jerry@example.com", issuer: "did:key:z6Mk1" };
  const signIn = (application?: string, withIdentity = true) => ({
    ...request(application),
    signIn: true,
    identity: withIdentity ? identity : undefined,
    requested: [{ type: ["Employee"], claims: ["email"] }],
  });

  it("says who the user would sign in as, with the email and the DID", () => {
    const p = signInPrompt(signIn("https://api.example"));
    expect(p.message).toBe("Aanmelden bij https://api.example?");
    expect(p.detail).toBe("E-mailadres: jerry@example.com\nDID: did:key:z6Mk1");
    expect(p.buttons).toEqual(["Bevestigen", "Annuleren"]);
  });

  it("is what a sign-in gets, and other requests keep their popup", () => {
    expect(promptFor(signIn("https://api.example")).signIn).toBe(true);
    expect(promptFor(request("https://api.example")).buttons).toEqual(["Delen", "Altijd delen", "Weigeren"]);
  });

  it("offers to sign in without the button next time only for an application the user can trust and an identity it knows", () => {
    expect(signInPrompt(signIn("https://api.example")).checkboxLabel).toBe("Altijd aanmelden, zonder de knop Aanmelden");
    expect(signInPrompt(signIn(undefined)).checkboxLabel).toBeUndefined();
    expect(signInPrompt(signIn("https://api.example", false)).checkboxLabel).toBeUndefined();
  });

  it("reads the answer from the button and the checkbox", () => {
    const p = signInPrompt(signIn("https://api.example"));
    expect(decision(p, 0, false)).toBe("share");
    expect(decision(p, 0, true)).toBe("always");
    expect(decision(p, 1, true)).toBe("decline");
    expect(decision(signInPrompt(signIn(undefined)), 0, true)).toBe("share");
  });
});
