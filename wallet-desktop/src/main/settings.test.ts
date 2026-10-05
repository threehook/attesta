import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { applicationOf, WalletSettings } from "./settings.js";

const file = () => join(mkdtempSync(join(tmpdir(), "attesta-settings-")), "settings.json");
const api = "https://api.attesta.example";

describe("applicationOf", () => {
  it("is the origin of the address the wallet answers to, not the request", () => {
    expect(applicationOf(`redirect_uri:${api}/v1/authorize/requests/abc/response`)).toBe(api);
    expect(applicationOf("redirect_uri:http://localhost:8080/response")).toBe("http://localhost:8080");
  });

  it("is nothing for other client id schemes or garbage", () => {
    expect(applicationOf("x509_san_dns:verifier.example")).toBeUndefined();
    expect(applicationOf(`${api}/response`)).toBeUndefined();
    expect(applicationOf("redirect_uri:not a url")).toBeUndefined();
    expect(applicationOf("redirect_uri:javascript:alert(1)")).toBeUndefined();
  });
});

describe("WalletSettings", () => {
  const jerry = { email: "jerry@example.com", issuer: "did:key:z6Mk1" };

  it("asks every time until the user chooses otherwise", () => {
    const s = new WalletSettings(file());
    expect(s.view()).toEqual({ trustedApplications: [], signIns: [] });
    expect(s.trusts(api)).toBe(false);
    expect(s.trusts(undefined)).toBe(false);
    expect(s.signsIn(api, jerry)).toBe(false);
  });

  it("shares without asking only with an application the user trusted", () => {
    const s = new WalletSettings(file());
    s.trust(api);
    expect(s.trusts(api)).toBe(true);
    expect(s.trusts("https://other.example")).toBe(false);
    expect(s.trusts(undefined)).toBe(false);
  });

  it("forgets a trusted application", () => {
    const s = new WalletSettings(file());
    s.trust(api);
    s.trust(api);
    expect(s.view().trustedApplications).toEqual([api]);
    s.forget(api);
    expect(s.trusts(api)).toBe(false);
  });

  it("signs in without asking only as the identity, and to the application, the user chose", () => {
    const s = new WalletSettings(file());
    s.rememberSignIn(api, jerry);
    s.rememberSignIn(api, jerry);
    expect(s.signsIn(api, jerry)).toBe(true);
    expect(s.signsIn(api, { ...jerry, email: "tom@example.com" })).toBe(false);
    expect(s.signsIn(api, { ...jerry, issuer: "did:key:z6Mk2" })).toBe(false);
    expect(s.signsIn("https://other.example", jerry)).toBe(false);
    expect(s.signsIn(undefined, jerry)).toBe(false);
    expect(s.signsIn(api, undefined)).toBe(false);
    expect(s.view().signIns).toEqual([{ application: api, ...jerry }]);
  });

  it("keeps signing in and sharing apart", () => {
    const s = new WalletSettings(file());
    s.rememberSignIn(api, jerry);
    expect(s.trusts(api)).toBe(false);
    s.trust("https://other.example");
    expect(s.signsIn("https://other.example", jerry)).toBe(false);
  });

  it("forgets one sign-in and leaves the others", () => {
    const s = new WalletSettings(file());
    const tom = { email: "tom@example.com", issuer: "did:key:z6Mk1" };
    s.rememberSignIn(api, jerry);
    s.rememberSignIn(api, tom);
    s.forgetSignIn({ application: api, ...jerry });
    expect(s.signsIn(api, jerry)).toBe(false);
    expect(s.signsIn(api, tom)).toBe(true);
  });

  it("keeps the choices in the file, readable by the user only", () => {
    const path = file();
    const s = new WalletSettings(path);
    s.trust(api);
    s.rememberSignIn(api, jerry);
    const saved = { trustedApplications: [api], signIns: [{ application: api, ...jerry }] };
    expect(new WalletSettings(path).view()).toEqual(saved);
    expect(JSON.parse(readFileSync(path, "utf8"))).toEqual(saved);
  });

  it("falls back to asking when the file is damaged", () => {
    const path = file();
    writeFileSync(path, "{ not json");
    expect(new WalletSettings(path).view()).toEqual({ trustedApplications: [], signIns: [] });
    writeFileSync(path, JSON.stringify({ trustedApplications: [1, api, null], signIns: [{ application: api, email: "a@b.c" }, null, { application: api, ...jerry }] }));
    expect(new WalletSettings(path).view()).toEqual({ trustedApplications: [api], signIns: [{ application: api, ...jerry }] });
  });

  it("ignores a switch to share with everyone that an earlier version may have saved", () => {
    const path = file();
    writeFileSync(path, JSON.stringify({ shareAutomatically: true, trustedApplications: [] }));
    const s = new WalletSettings(path);
    expect(s.trusts("https://other.example")).toBe(false);
    s.trust(api);
    expect(JSON.parse(readFileSync(path, "utf8"))).toEqual({ trustedApplications: [api], signIns: [] });
  });
});
