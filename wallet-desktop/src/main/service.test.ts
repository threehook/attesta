import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import type { WalletAgent } from "@attesta/wallet";
import { beforeEach, describe, expect, it, vi } from "vitest";

const core = vi.hoisted(() => ({
  resolvePresentationRequest: vi.fn(),
  submitPresentation: vi.fn(),
  listCredentials: vi.fn(),
  removeCredential: vi.fn(),
  previewCredential: vi.fn(),
  acceptPreviewed: vi.fn(),
  readQrImage: vi.fn(),
}));
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
  core.removeCredential.mockReset().mockResolvedValue(undefined);
  core.submitPresentation.mockReset().mockResolvedValue({ status: 200 });
  core.previewCredential.mockReset();
  core.acceptPreviewed.mockReset().mockResolvedValue([]);
  core.readQrImage.mockReset();
});

// Reading real links and requests is covered end to end (e2e/wallet.spec.ts); here the wallet core is replaced to cover what the service decides.
describe("WalletService", () => {
  it("rejects a link that is neither a credential link nor a presentation request", async () => {
    await expect(service().prepare("https://example.com")).rejects.toThrow(WalletError);
    await expect(service().prepare("")).rejects.toThrow("geen credential-link");
  });

  it("rejects approving something it never prepared", async () => {
    await expect(service().approve("unknown")).rejects.toThrow("verlopen");
  });

  it("removes a credential by its id", async () => {
    const agent = {} as WalletAgent;
    await expect(new WalletService(agent, settings()).remove("c-1")).resolves.toBeNull();
    expect(core.removeCredential).toHaveBeenCalledWith(agent, "c-1");
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

const issuer = "https://issuer.example/oid4vci/diploma";
const qrLink = "openid-credential-offer://?credential_offer_uri=x";
const previewed = { issuer, types: ["Employee"], resolved: {}, replaces: [], replaceHeld: false };
const image = new Uint8Array([1, 2, 3]);

describe("WalletService, a QR code in an image", () => {
  it("reads the link in the QR code and prepares the credential in it", async () => {
    core.readQrImage.mockReturnValue(qrLink);
    core.previewCredential.mockResolvedValue(previewed);

    const prepared = await service().prepareImage(image);

    expect(core.readQrImage).toHaveBeenCalledWith(image);
    expect(core.previewCredential).toHaveBeenCalledWith({}, qrLink);
    expect(prepared).toMatchObject({ kind: "credential", issuer, types: ["Employee"] });
    expect(core.acceptPreviewed).not.toHaveBeenCalled();
  });

  it("tells which revoked or ended credentials the new one replaces, by title", async () => {
    core.readQrImage.mockReturnValue(qrLink);
    core.previewCredential.mockResolvedValue({ ...previewed, replaces: [{ id: "1", type: "Employee", issuer: "did:key:z6", claims: { title: "Medewerker afdeling & rollen" } }] });

    expect(await service().prepareImage(image)).toMatchObject({ kind: "credential", replaces: ["Medewerker afdeling & rollen"] });
  });

  it("adds nothing until the user agrees", async () => {
    core.readQrImage.mockReturnValue(qrLink);
    core.previewCredential.mockResolvedValue(previewed);
    core.acceptPreviewed.mockResolvedValue([{ id: "1" }]);
    const s = service();
    const prepared = (await s.prepareImage(image)) as { id: string };

    expect(await s.approve(prepared.id)).toEqual({ kind: "credential", credentials: [{ id: "1" }] });
  });

  it.each([
    ["something that is not bytes", "not bytes"],
    ["an empty file", new Uint8Array()],
    ["a file that is too big", new Uint8Array(10 * 1024 * 1024 + 1)],
  ])("refuses %s without looking at it", async (_name, input) => {
    await expect(service().prepareImage(input)).rejects.toThrow("geen afbeelding met een QR-code");
    expect(core.readQrImage).not.toHaveBeenCalled();
  });

  it("says why an image cannot be read", async () => {
    core.readQrImage.mockImplementation(() => {
      throw new Error("Er staat geen QR-code in deze afbeelding.");
    });

    await expect(service().prepareImage(image)).rejects.toThrow("Er staat geen QR-code in deze afbeelding.");
  });

  it("passes on what the issuer says about a QR code that has expired", async () => {
    core.readQrImage.mockReturnValue(qrLink);
    core.previewCredential.mockRejectedValue(new Error("De QR-code is verlopen. Er is een nieuwe QR-code naar uw e-mail gestuurd."));

    await expect(service().prepareImage(image)).rejects.toThrow("Er is een nieuwe QR-code naar uw e-mail gestuurd.");
  });

  it("tells the user when the issuer has no credential for them", async () => {
    core.readQrImage.mockReturnValue(qrLink);
    core.previewCredential.mockResolvedValue(previewed);
    core.acceptPreviewed.mockRejectedValue(Object.assign(new Error("request failed"), { response: { credentialErrorResponseResult: { data: { error: "credential_request_denied" } } } }));
    const s = service();
    const prepared = (await s.prepareImage(image)) as { id: string };

    await expect(s.approve(prepared.id)).rejects.toThrow("Er staat geen credential voor u klaar");
  });

  it("passes on any other failure as it is", async () => {
    core.readQrImage.mockReturnValue(qrLink);
    core.previewCredential.mockResolvedValue(previewed);
    core.acceptPreviewed.mockRejectedValue(new Error("the issuer did not answer"));
    const s = service();
    const prepared = (await s.prepareImage(image)) as { id: string };

    await expect(s.approve(prepared.id)).rejects.toThrow("did not answer");
  });
});

