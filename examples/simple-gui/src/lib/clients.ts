// Singletons shared across the app, and the diploma_membership circuit's static assets (synced by sync-circuit).
import { ApiClient, Wallet } from "@zk-puoi/client";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export const apiClient = new ApiClient(API_BASE_URL);
export const wallet = new Wallet();

export const CIRCUIT_WASM_PATH = "/circuits/diploma_membership/diploma_membership.wasm";
export const CIRCUIT_ZKEY_PATH = "/circuits/diploma_membership/diploma_membership_final.zkey";
const REGISTRY_PATH = "/circuits/diploma_membership/registry.json";

export interface RegistryCredentialInput {
  credType: string;
  issuer: string;
  subject: string;
  salt: string;
  pathElements: string[];
  pathIndices: number[];
}

export interface RegistryCredential {
  id: string;
  type: string;
  issuer: string;
  subject: string;
  privateInput: RegistryCredentialInput;
}

export interface Registry {
  root: string;
  credentials: RegistryCredential[];
}

export async function loadRegistry(): Promise<Registry> {
  const res = await fetch(REGISTRY_PATH);
  if (!res.ok) {
    throw new Error(`failed to load credential registry: ${res.status}`);
  }
  return res.json();
}
