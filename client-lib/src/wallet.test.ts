import { describe, expect, it } from "vitest";
import { Credential, InMemoryStorage, Wallet } from "./wallet.js";

const DIPLOMA: Credential = {
  id: "diploma-1",
  type: "Diploma",
  issuer: "trusted-university",
  subject: "alice",
  claims: { degree: "BSc Computer Science" },
};

describe("Wallet", () => {
  it("starts empty", () => {
    const wallet = new Wallet(new InMemoryStorage());
    expect(wallet.list()).toEqual([]);
    expect(wallet.get("diploma-1")).toBeUndefined();
  });

  it("adds and retrieves a credential", () => {
    const wallet = new Wallet(new InMemoryStorage());
    wallet.add(DIPLOMA);

    expect(wallet.list()).toEqual([DIPLOMA]);
    expect(wallet.get("diploma-1")).toEqual(DIPLOMA);
  });

  it("replaces a credential added twice under the same id", () => {
    const wallet = new Wallet(new InMemoryStorage());
    wallet.add(DIPLOMA);
    wallet.add({ ...DIPLOMA, issuer: "diploma-mill" });

    expect(wallet.list()).toHaveLength(1);
    expect(wallet.get("diploma-1")?.issuer).toBe("diploma-mill");
  });

  it("removes a credential", () => {
    const wallet = new Wallet(new InMemoryStorage());
    wallet.add(DIPLOMA);
    wallet.remove("diploma-1");

    expect(wallet.list()).toEqual([]);
  });

  it("persists across instances sharing the same storage", () => {
    const storage = new InMemoryStorage();
    new Wallet(storage).add(DIPLOMA);

    expect(new Wallet(storage).list()).toEqual([DIPLOMA]);
  });
});
