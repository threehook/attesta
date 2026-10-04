import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { loadOrCreateStoreKey, type KeyProtector } from "./keystore.js";

// Stands in for the OS keychain: "encrypts" by reversing the text, which is enough to tell stored bytes from the key itself.
const protector = (available = true): KeyProtector => ({
  isAvailable: () => available,
  encrypt: (plain) => Buffer.from([...plain].reverse().join("")),
  decrypt: (encrypted) => [...encrypted.toString()].reverse().join(""),
});

const fileIn = () => join(mkdtempSync(join(tmpdir(), "keystore-")), "nested", "store-key");

describe("loadOrCreateStoreKey", () => {
  it("creates a key on first use and returns the same one afterwards", () => {
    const file = fileIn();

    const first = loadOrCreateStoreKey(file, protector());
    const second = loadOrCreateStoreKey(file, protector());

    expect(first.length).toBeGreaterThanOrEqual(32);
    expect(second).toBe(first);
  });

  it("never writes the key to disk in the clear", () => {
    const file = fileIn();

    const key = loadOrCreateStoreKey(file, protector());

    expect(readFileSync(file).toString()).not.toContain(key);
  });

  it("gives different wallets different keys", () => {
    expect(loadOrCreateStoreKey(fileIn(), protector())).not.toBe(loadOrCreateStoreKey(fileIn(), protector()));
  });

  it("refuses to run without secure storage", () => {
    expect(() => loadOrCreateStoreKey(fileIn(), protector(false))).toThrow("secure storage");
  });
});
