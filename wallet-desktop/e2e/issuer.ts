// A real issuer (the Go binary) for the end-to-end tests, with its own database and its own sender address in the shared mail catcher.
import { type ChildProcess, execFileSync, spawn } from "node:child_process";
import { createServer } from "node:net";
import { readQrImage } from "@attesta/wallet";

export interface DiplomaClaims {
  name: string;
  email: string;
  degree: string;
  university: string;
}

export interface EmployeeClaims {
  name: string;
  email: string;
  department: string;
  organisation: string;
  diploma: string;
  diplomaValidUntil: string;
}

export interface TestIssuerOptions {
  qrLifetimeSeconds?: number;
  /** A duration such as "500ms". */
  renewalCooldown?: string;
}

export interface TestIssuer {
  /** The address wallets reach the issuer at. */
  url: string;
  /** The issuer's DID, which verifiers trust it by. */
  did: string;
  /** Records the entitlement; the issuer mails its person a QR code. Gives the entitlement's id. */
  recordDiploma(claims: DiplomaClaims): Promise<string>;
  recordEmployee(claims: EmployeeClaims): Promise<string>;
  /** Withdraws an entitlement that was not issued yet. */
  revoke(entitlementId: string): Promise<void>;
  /** The number of mails this issuer sent. */
  mailCount(): Promise<number>;
  /** The QR code image in the newest mail to `to`, as the person would save it from the mail. */
  qrImageFor(to: string): Promise<Buffer>;
  /** The link in that QR code. */
  qrLinkFor(to: string): Promise<string>;
  close(): Promise<void>;
}

const env = (name: string) => {
  const value = process.env[name];
  if (!value) throw new Error(`${name} is not set; the end-to-end tests start it in global-setup.ts`);
  return value;
};

async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, resolve));
  const { port } = server.address() as { port: number };
  await new Promise((resolve) => server.close(resolve));
  return port;
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));
let started = 0;

export async function startTestIssuer(options: TestIssuerOptions = {}): Promise<TestIssuer> {
  const n = ++started;
  const database = `e2e_${process.pid}_${n}`;
  const from = `issuer-${process.pid}-${n}@e2e.test`;
  const port = await freePort();
  const url = `http://localhost:${port}`;
  const mailApi = env("ATTESTA_E2E_MAIL_API");

  const output: string[] = [];
  const child: ChildProcess = spawn(env("ATTESTA_E2E_ISSUER_BIN"), [], {
    env: {
      ...process.env,
      ATTESTA_ISSUER_PORT: String(port),
      ATTESTA_ISSUER_PUBLIC_URL: url,
      ATTESTA_ISSUER_AUTH_DISABLED: "true",
      ATTESTA_ISSUER_DB_HOST: `127.0.0.1:${env("ATTESTA_E2E_POSTGRES_PORT")}`,
      ATTESTA_ISSUER_DB_USER: "postgres",
      ATTESTA_ISSUER_DB_PASSWORD: "e2e",
      ATTESTA_ISSUER_DB_NAME: database,
      ATTESTA_ISSUER_SMTP_HOST: "127.0.0.1",
      ATTESTA_ISSUER_SMTP_PORT: env("ATTESTA_E2E_SMTP_PORT"),
      ATTESTA_ISSUER_SMTP_TLS: "none",
      ATTESTA_ISSUER_MAIL_FROM: from,
      ...(options.qrLifetimeSeconds ? { ATTESTA_ISSUER_QR_LIFETIME_SECONDS: String(options.qrLifetimeSeconds) } : {}),
      ...(options.renewalCooldown ? { ATTESTA_ISSUER_RENEWAL_COOLDOWN: options.renewalCooldown } : {}),
    },
  });
  child.stdout?.on("data", (chunk) => output.push(String(chunk)));
  child.stderr?.on("data", (chunk) => output.push(String(chunk)));
  const exited = new Promise<void>((resolve) => child.once("exit", () => resolve()));

  const close = async () => {
    child.kill("SIGTERM");
    await exited;
  };

  let did = "";
  for (let i = 0; ; i++) {
    if (child.exitCode !== null || i > 100) {
      await close();
      throw new Error(`the issuer did not start:\n${output.join("")}`);
    }
    try {
      const response = await fetch(`${url}/info`);
      if (response.ok) {
        did = ((await response.json()) as { did: string }).did;
        break;
      }
    } catch {
      // not listening yet
    }
    await sleep(100);
  }

  const record = async (path: string, claims: DiplomaClaims | EmployeeClaims): Promise<string> => {
    const response = await fetch(`${url}${path}`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(claims) });
    const body = (await response.json()) as { entitlementId?: string; error?: string };
    if (!response.ok || !body.entitlementId) throw new Error(`recording ${claims.email} failed: ${response.status} ${body.error}`);
    return body.entitlementId;
  };

  const search = async (query: string) => {
    const response = await fetch(`${mailApi}/api/v1/search?limit=500&query=${encodeURIComponent(query)}`);
    return ((await response.json()) as { messages: { ID: string }[] }).messages;
  };

  const qrImageFor = async (to: string): Promise<Buffer> => {
    for (let i = 0; i < 20; i++) {
      const [newest] = await search(`from:${from} to:${to}`);
      if (newest) {
        const message = (await (await fetch(`${mailApi}/api/v1/message/${newest.ID}`)).json()) as { Inline?: { PartID: string; ContentType: string }[] };
        const image = message.Inline?.find((part) => part.ContentType === "image/png");
        if (image) return Buffer.from(await (await fetch(`${mailApi}/api/v1/message/${newest.ID}/part/${image.PartID}`)).arrayBuffer());
      }
      await sleep(100);
    }
    throw new Error(`no mail with a QR code was sent to ${to}`);
  };

  return {
    url,
    did,
    recordDiploma: (claims) => record("/entitlements/diploma", claims),
    recordEmployee: (claims) => record("/entitlements/employee", claims),
    async revoke(entitlementId) {
      execFileSync("docker", ["exec", env("ATTESTA_E2E_POSTGRES"), "psql", "-U", "postgres", "-d", database, "-c", `UPDATE entitlements SET status = 'revoked' WHERE id = '${entitlementId}' AND status IN ('open', 'claimed')`]);
    },
    mailCount: async () => (await search(`from:${from}`)).length,
    qrImageFor,
    async qrLinkFor(to) {
      return readQrImage(await qrImageFor(to));
    },
    close,
  };
}
