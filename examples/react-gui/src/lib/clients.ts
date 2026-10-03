// Singletons shared across the app: one ApiClient (talking to the real backend) and one Wallet (backed by the
// browser's localStorage via client-lib's default Storage).
import { ApiClient, Wallet } from "@zk-puoi/client";

const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "http://localhost:8080";

export const apiClient = new ApiClient(API_BASE_URL);
export const wallet = new Wallet();

// Paths sync-circuit copies client-lib's committed circuit artifacts to (see package.json) — served as static
// files by Vite, not bundled, since snarkjs loads them itself via fetch.
export const CUBIC_WASM_PATH = "/circuits/cubic/cubic.wasm";
export const CUBIC_ZKEY_PATH = "/circuits/cubic/cubic_final.zkey";
