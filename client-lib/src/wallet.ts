// Mock Verifiable Credential wallet: holds simplified JSON objects shaped like VCs (issuer, subject, claims), not real W3C
// VC-JWT/Data-Integrity-proof credentials — the lightweight-mock approach chosen for this project's MVP. No DID resolution, no signatures: a
// credential here is only as trustworthy as whoever put it in storage.

export interface Credential {
  id: string;
  type: string;
  issuer: string;
  subject: string;
  claims: Record<string, unknown>;
}

// Storage is the minimal persistence interface Wallet needs; the Web Storage API (localStorage/sessionStorage) already satisfies it. InMemoryStorage
// below covers Node/tests, where no such global exists.
export interface Storage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export class InMemoryStorage implements Storage {
  private readonly data = new Map<string, string>();

  getItem(key: string): string | null {
    return this.data.has(key) ? this.data.get(key)! : null;
  }

  setItem(key: string, value: string): void {
    this.data.set(key, value);
  }

  removeItem(key: string): void {
    this.data.delete(key);
  }
}

// defaultStorage picks the browser's localStorage when available, falling back to an in-memory store otherwise (Node, or a browser context where
// storage access is blocked) rather than throwing at import time.
function defaultStorage(): Storage {
  if (typeof localStorage !== "undefined") {
    return localStorage;
  }
  return new InMemoryStorage();
}

const STORAGE_KEY = "zk-puoi.wallet.credentials";

export class Wallet {
  constructor(private readonly storage: Storage = defaultStorage()) {}

  list(): Credential[] {
    const raw = this.storage.getItem(STORAGE_KEY);
    return raw ? (JSON.parse(raw) as Credential[]) : [];
  }

  get(id: string): Credential | undefined {
    return this.list().find((c) => c.id === id);
  }

  add(credential: Credential): void {
    const credentials = this.list().filter((c) => c.id !== credential.id);
    credentials.push(credential);
    this.save(credentials);
  }

  remove(id: string): void {
    this.save(this.list().filter((c) => c.id !== id));
  }

  private save(credentials: Credential[]): void {
    this.storage.setItem(STORAGE_KEY, JSON.stringify(credentials));
  }
}
