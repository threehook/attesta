import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { WalletAgent } from "@attesta/wallet";
import { beforeEach, describe, expect, it, vi } from "vitest";

const core = vi.hoisted(() => ({ resolvePresentationRequest: vi.fn(), submitPresentation: vi.fn(), listCredentials: vi.fn() }));
vi.mock("@attesta/wallet", async (importOriginal) => ({ ...(await importOriginal<object>()), ...core }));

import { WalletError, WalletService } from "./service.js";
import { WalletSettings } from "./settings.js";

const api = "https://api.attesta.example";
const link = "openid4vp://?x=1";
const request = (overrides: object = {}) => ({
  verifier: `redirect_uri:${api}/v1/authorize/requests/r1/response`,
  requested: [{ type: ["Employee"], claims: ["department", "diploma"] }],
  satisfiable: true,
  ...overrides,
});

const signIn = (overrides: object = {}) => request({ requested: [{ type: ["Employee"], claims: ["email"] }], ...overrides });

const settings = () => new WalletSettings(join(mkdtempSync(join(tmpdir(), "attesta-service-")), "settings.json"));
const service = (s = settings()) => new WalletService({} as WalletAgent, s);

beforeEach(() => {
  core.resolvePresentationRequest.mockReset();
  core.listCredentials.mockReset().mockResolvedValue([]);
  core.submitPresentation.mockReset().mockResolvedValue({ status: 200 });
});

// Reading real offers and requests is covered end to end (e2e/wallet.spec.ts); here the wallet core is replaced to cover what the service decides.
describe("WalletService", () => {
  it("rejects a link that is neither an offer nor a presentation request", async () => {
    await expect(service().prepare("https://example.com")).rejects.toThrow(WalletError);
    await expect(service().prepare("")).rejects.toThrow("not a credential offer");
  });

  it("rejects approving something it never prepared", async () => {
    await expect(service().approve("unknown")).rejects.toThrow("expired");
  });

  it("forgets what was declined", async () => {
    const s = service();
    s.decline("anything");
    await expect(s.approve("anything")).rejects.toThrow(WalletError);
  });
});

describe("sharing without asking", () => {
  it("asks first by default and shares only once the user approves", async () => {
    core.resolvePresentationRequest.mockResolvedValue(request());
    const s = service();
    const prepared = await s.prepare(link);
    expect(prepared).toMatchObject({ kind: "presentation", application: api });
    expect(core.submitPresentation).not.toHaveBeenCalled();
    if (prepared.kind !== "presentation") throw new Error("expected a presentation");
    await expect(s.approve(prepared.id)).resolves.toEqual({ kind: "presentation", status: 200 });
    expect(core.submitPresentation).toHaveBeenCalledTimes(1);
  });

  it("makes the application one that is answered without asking when the user asks to remember it", async () => {
    core.resolvePresentationRequest.mockResolvedValue(request());
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id, true);

    const again = await s.prepare(link);
    expect(again).toEqual({ kind: "shared", verifier: expect.any(String), claims: ["department", "diploma"], status: 200 });
    expect(core.submitPresentation).toHaveBeenCalledTimes(2);
  });

  it("does not remember an application the user did not ask to remember", async () => {
    core.resolvePresentationRequest.mockResolvedValue(request());
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id);
    expect((await s.prepare(link)).kind).toBe("presentation");
  });

  it("shares without asking only with the application the user chose", async () => {
    core.resolvePresentationRequest.mockResolvedValue(request());
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id, true);

    core.resolvePresentationRequest.mockResolvedValue(request({ verifier: "redirect_uri:https://other.example/response" }));
    expect((await s.prepare(link)).kind).toBe("presentation");
  });

  it("still asks, and says so, when the wallet holds nothing that answers the request", async () => {
    core.resolvePresentationRequest.mockResolvedValue(request({ satisfiable: false }));
    const trusted = settings();
    trusted.trust(api);
    const s = service(trusted);
    expect(await s.prepare(link)).toMatchObject({ kind: "presentation", satisfiable: false });
    expect(core.submitPresentation).not.toHaveBeenCalled();
  });

  it("stops sharing without asking once the user removes the application", async () => {
    core.resolvePresentationRequest.mockResolvedValue(request());
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id, true);
    expect(s.settingsView().trustedApplications).toEqual([api]);
    expect(s.forgetApplication(api).trustedApplications).toEqual([]);
    expect((await s.prepare(link)).kind).toBe("presentation");
  });

  it("does not trust an application when the answer could not be sent", async () => {
    core.resolvePresentationRequest.mockResolvedValue(request());
    core.submitPresentation.mockRejectedValue(new Error("connection refused"));
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await expect(s.approve(first.id, true)).rejects.toThrow("connection refused");
    expect(s.settingsView().trustedApplications).toEqual([]);
  });
});

describe("signing in", () => {
  const jerry = { email: "jerry@example.com", issuer: "did:key:z6Mk1" };
  const held = (email: string, issuer = jerry.issuer, type = "Employee") => ({ id: `${email}-${issuer}`, type, issuer, claims: { email } });
  const withCredentials = (...credentials: object[]) => {
    core.listCredentials.mockResolvedValue(credentials);
  };

  it("treats a request for nothing but the email as a sign-in, and anything more as a request", async () => {
    withCredentials(held(jerry.email));
    core.resolvePresentationRequest.mockResolvedValue(signIn());
    expect(await service().prepare(link)).toMatchObject({ kind: "presentation", signIn: true });
    core.resolvePresentationRequest.mockResolvedValue(request());
    expect(await service().prepare(link)).toMatchObject({ kind: "presentation", signIn: false, identity: undefined });
  });

  it("names the identity the link chose, or else the first credential of the type asked for", async () => {
    withCredentials(held("tom@example.com"), held(jerry.email), held("ada@example.com", jerry.issuer, "Diploma"));
    core.resolvePresentationRequest.mockResolvedValue(signIn({ hint: jerry }));
    expect(await service().prepare(link)).toMatchObject({ identity: jerry });
    core.resolvePresentationRequest.mockResolvedValue(signIn());
    expect(await service().prepare(link)).toMatchObject({ identity: { email: "tom@example.com", issuer: jerry.issuer } });
  });

  it("lists identities: an email and its issuer, once each, of the type asked for", async () => {
    withCredentials(held(jerry.email), held(jerry.email), held("ada@example.com", "did:key:z6Mk2", "Diploma"), { id: "x", type: "Employee", issuer: "i", claims: {} });
    expect(await service().identities("Employee")).toEqual([jerry]);
    expect(await service().identities()).toEqual([jerry, { email: "ada@example.com", issuer: "did:key:z6Mk2" }]);
  });

  it("signs in without asking once the user chose to, as that identity only, and still asks about what an employee may do", async () => {
    withCredentials(held(jerry.email), held("tom@example.com"));
    core.resolvePresentationRequest.mockResolvedValue(signIn({ hint: jerry }));
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id, true);
    expect(s.settingsView()).toEqual({ trustedApplications: [], signIns: [{ application: api, ...jerry }] });

    expect(await s.prepare(link)).toEqual({ kind: "shared", verifier: expect.any(String), claims: ["email"], status: 200 });
    core.resolvePresentationRequest.mockResolvedValue(signIn({ hint: { email: "tom@example.com", issuer: jerry.issuer } }));
    expect((await s.prepare(link)).kind).toBe("presentation");
    core.resolvePresentationRequest.mockResolvedValue(request());
    expect((await s.prepare(link)).kind).toBe("presentation");
  });

  it("does not sign in without asking because requests are always shared", async () => {
    withCredentials(held(jerry.email));
    core.resolvePresentationRequest.mockResolvedValue(request());
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id, true);

    core.resolvePresentationRequest.mockResolvedValue(signIn({ hint: jerry }));
    expect((await s.prepare(link)).kind).toBe("presentation");
  });

  it("tells a page which identity signs in automatically to which application", async () => {
    withCredentials(held(jerry.email), held("tom@example.com"));
    core.resolvePresentationRequest.mockResolvedValue(signIn({ hint: jerry }));
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id, true);
    expect(s.signsIn(api, jerry)).toBe(true);
    expect(s.signsIn(api, { email: "tom@example.com", issuer: jerry.issuer })).toBe(false);
    expect(s.signsIn("https://other.example", jerry)).toBe(false);
  });

  it("stops signing in without asking once the user removes it", async () => {
    withCredentials(held(jerry.email));
    core.resolvePresentationRequest.mockResolvedValue(signIn({ hint: jerry }));
    const s = service();
    const first = await s.prepare(link);
    if (first.kind !== "presentation") throw new Error("expected a presentation");
    await s.approve(first.id, true);
    expect(s.forgetSignIn({ application: api, ...jerry }).signIns).toEqual([]);
    expect((await s.prepare(link)).kind).toBe("presentation");
  });
});
