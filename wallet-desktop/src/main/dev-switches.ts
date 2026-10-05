// The environment variables for development and tests: a fixed store key, a different data folder, plain http, and another port for pages. They apply only when running
// from source; an installed app ignores them, so nothing that can set its environment can change where it keeps its data, which key protects it,
// or which servers it trusts over http.
export interface DevSwitches {
  /** Replaces the wallet's default data folder. */
  dataDir?: string;
  /** Replaces the key kept under the system keychain. */
  storeKey?: string;
  /** Accept issuers and verifiers served over plain http. */
  allowInsecureHttp: boolean;
  /** Replaces the port pages find the wallet on. */
  identityPort?: number;
}

export function devSwitches(env: NodeJS.ProcessEnv, packaged: boolean): DevSwitches {
  if (packaged) {
    return { allowInsecureHttp: false };
  }
  return {
    dataDir: env.ATTESTA_WALLET_DATA_DIR || undefined,
    storeKey: env.ATTESTA_WALLET_KEY || undefined,
    allowInsecureHttp: env.ATTESTA_ALLOW_INSECURE_HTTP === "1",
    identityPort: /^\d+$/.test(env.ATTESTA_WALLET_PORT ?? "") ? Number(env.ATTESTA_WALLET_PORT) : undefined,
  };
}
