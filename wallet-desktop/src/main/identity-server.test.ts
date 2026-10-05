import type { AddressInfo } from "node:net";
import type { Server } from "node:http";
import { afterEach, describe, expect, it } from "vitest";
import { identityList, startIdentityServer, type IdentityStore } from "./identity-server.js";

const jerry = { email: "jerry@example.com", issuer: "did:key:z6Mk1" };
const api = "https://api.example";

const store = (overrides: Partial<IdentityStore> = {}): IdentityStore & { asked: Array<string | undefined> } => {
  const asked: Array<string | undefined> = [];
  return {
    asked,
    identities: async (type) => {
      asked.push(type);
      return [jerry, { email: "tom@example.com", issuer: "did:key:z6Mk2" }];
    },
    signsIn: (application, identity) => application === api && identity.email === jerry.email,
    ...overrides,
  };
};

describe("identityList", () => {
  it("says which identity signs in automatically to the named application", async () => {
    expect(await identityList(store(), "Employee", api)).toEqual({
      identities: [
        { ...jerry, automatic: true },
        { email: "tom@example.com", issuer: "did:key:z6Mk2", automatic: false },
      ],
    });
  });

  it("says none does for an application it does not know", async () => {
    const list = await identityList(store(), undefined, undefined);
    expect(list.identities.every((i) => !i.automatic)).toBe(true);
  });
});

describe("startIdentityServer", () => {
  let server: Server | undefined;
  afterEach(() => new Promise<void>((resolve) => (server ? server.close(() => resolve()) : resolve())));

  const start = async (s: IdentityStore) => {
    server = await startIdentityServer(s, 0);
    return `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
  };

  it("listens on the loopback address only", async () => {
    await start(store());
    expect((server!.address() as AddressInfo).address).toBe("127.0.0.1");
  });

  it("answers a page on any origin with the identities, and asks for the type and application the page named", async () => {
    const s = store();
    const base = await start(s);
    const res = await fetch(`${base}/identities?type=Employee&application=${encodeURIComponent(api)}`, { headers: { Origin: "https://page.example" } });
    expect(res.status).toBe(200);
    expect(res.headers.get("access-control-allow-origin")).toBe("*");
    expect(res.headers.get("cache-control")).toBe("no-store");
    expect(await res.json()).toEqual({
      identities: [
        { ...jerry, automatic: true },
        { email: "tom@example.com", issuer: "did:key:z6Mk2", automatic: false },
      ],
    });
    expect(s.asked).toEqual(["Employee"]);
  });

  it("answers the preflight a browser sends to reach the local machine", async () => {
    const base = await start(store());
    const res = await fetch(`${base}/identities`, { method: "OPTIONS" });
    expect(res.status).toBe(204);
    expect(res.headers.get("access-control-allow-private-network")).toBe("true");
  });

  it("offers nothing but the list", async () => {
    const base = await start(store());
    expect((await fetch(`${base}/credentials`)).status).toBe(404);
    expect((await fetch(`${base}/identities`, { method: "POST" })).status).toBe(404);
  });

  it("answers 500 without details when the wallet cannot list its credentials", async () => {
    const base = await start(store({ identities: async () => Promise.reject(new Error("store locked")) }));
    const res = await fetch(`${base}/identities`);
    expect(res.status).toBe(500);
    expect(await res.text()).toBe("");
  });

  it("refuses to start when the port is taken", async () => {
    const base = await start(store());
    const port = Number(new URL(base).port);
    await expect(startIdentityServer(store(), port)).rejects.toThrow();
  });
});
