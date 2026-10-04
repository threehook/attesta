/// <reference types="vite/client" />
import type { WalletApi } from "../shared/api.js";

declare global {
  interface Window {
    wallet: WalletApi;
  }
}
