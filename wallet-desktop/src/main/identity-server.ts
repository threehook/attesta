import { createServer, type Server } from "node:http";
import type { Identity } from "../shared/api.js";
import { applicationOf } from "./settings.js";

// The port a page finds the wallet on. Pages on this machine ask it who the user is; it listens on the loopback address only.
export const IDENTITY_PORT = 47653;

export interface IdentityStore {
  identities(type?: string): Promise<Identity[]>;
  signsIn(application: string | undefined, identity: Identity): boolean;
}

export interface IdentityList {
  identities: Array<Identity & { /** Whether this identity signs in to the asking application without asking. */ automatic: boolean }>;
}

// What a page may learn: the email and issuer of each identity, and whether it signs in automatically to the application the page names. The page
// names it (an origin, as the wallet knows applications); nothing else about the wallet is shown.
export async function identityList(store: IdentityStore, type: string | undefined, application: string | undefined): Promise<IdentityList> {
  const identities = await store.identities(type);
  return { identities: identities.map((identity) => ({ ...identity, automatic: store.signsIn(application, identity) })) };
}

const CORS = {
  "Access-Control-Allow-Origin": "*",
  "Access-Control-Allow-Methods": "GET",
  "Access-Control-Allow-Private-Network": "true",
  "Cache-Control": "no-store",
};

export function startIdentityServer(store: IdentityStore, port: number, log: (message: string) => void = () => {}): Promise<Server> {
  const server = createServer((req, res) => {
    const url = new URL(req.url ?? "/", "http://127.0.0.1");
    if (req.method === "OPTIONS") {
      res.writeHead(204, CORS).end();
    } else if (req.method === "GET" && url.pathname === "/identities") {
      const application = applicationOf(`redirect_uri:${url.searchParams.get("application") ?? ""}`);
      identityList(store, url.searchParams.get("type") ?? undefined, application).then(
        (list) => res.writeHead(200, { ...CORS, "Content-Type": "application/json" }).end(JSON.stringify(list)),
        (error) => {
          log(`identities failed: ${error instanceof Error ? error.message : String(error)}`);
          res.writeHead(500, CORS).end();
        },
      );
    } else {
      res.writeHead(404, CORS).end();
    }
  });
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => resolve(server));
  });
}
