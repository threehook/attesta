import { randomBytes } from "node:crypto";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname } from "node:path";

// KeyProtector encrypts a secret with something only this user's login on this machine can unlock; Electron's safeStorage (the OS keychain)
// is the implementation.
export interface KeyProtector {
  isAvailable(): boolean;
  encrypt(plain: string): Buffer;
  decrypt(encrypted: Buffer): string;
}

// loadOrCreateStoreKey returns the key that encrypts the wallet store. It is generated on first use and kept at file, encrypted by the protector,
// so the user is never asked for a password and the key never sits on disk in the clear.
export function loadOrCreateStoreKey(file: string, protector: KeyProtector): string {
  if (!protector.isAvailable()) {
    throw new Error("The operating system's secure storage is not available, so the wallet cannot protect its key.");
  }
  if (existsSync(file)) {
    return protector.decrypt(readFileSync(file));
  }
  const key = randomBytes(32).toString("base64");
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, protector.encrypt(key), { mode: 0o600 });
  return key;
}
