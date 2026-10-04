// Runs the real Electron app against a real issuer and a stand-in verifier, and drives its window like a user would.
import { mkdtempSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { _electron as electron, expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { startIssuer } from "../../examples/issuer/src/issuer.js";

const appDir = join(import.meta.dirname, "..");

async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, resolve));
  const { port } = server.address() as AddressInfo;
  await new Promise((resolve) => server.close(resolve));
  return port;
}

test.describe.serial("desktop wallet", () => {
  let issuer: Awaited<ReturnType<typeof startIssuer>>;
  let verifier: Server;
  let responseUri: string;
  const answers: URLSearchParams[] = [];
  let app: ElectronApplication;
  let window: Page;

  test.beforeAll(async () => {
    const issuerPort = await freePort();
    issuer = await startIssuer({ port: issuerPort, publicUrl: `http://localhost:${issuerPort}`, storeKey: "e2e", allowInsecureHttp: true });

    verifier = createServer((req, res) => {
      let body = "";
      req.on("data", (chunk) => {
        body += chunk;
      });
      req.on("end", () => {
        answers.push(new URLSearchParams(body));
        res.writeHead(200, { "Content-Type": "application/json" }).end("{}");
      });
    });
    await new Promise<void>((resolve) => verifier.listen(0, resolve));
    responseUri = `http://localhost:${(verifier.address() as AddressInfo).port}/response`;

    app = await electron.launch({
      args: [appDir],
      env: {
        ...process.env,
        ZKPUOI_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "zk-puoi-wallet-")),
        ZKPUOI_WALLET_KEY: "e2e-wallet-key",
        ZKPUOI_ALLOW_INSECURE_HTTP: "1",
      },
    });
    window = await app.firstWindow();
  });

  test.afterAll(async () => {
    await app?.close();
    verifier?.close();
    await issuer?.close();
  });

  const openLink = async (link: string) => {
    await window.getByLabel("Link").fill(link);
    await window.getByRole("button", { name: "Open" }).click();
  };

  test("starts empty", async () => {
    await expect(window.getByText("No credentials yet")).toBeVisible();
  });

  test("rejects a link it does not understand", async () => {
    await openLink("https://example.com");
    await expect(window.getByRole("status")).toContainText("not a credential offer");
  });

  test("shows an offer, and adds the credential only once the user agrees", async () => {
    const offer = await issuer.createOffer({ name: "Ada Lovelace", email: "ada@example.com", degree: "Mathematics", university: "Trusted University" });

    await openLink(offer);
    const dialog = window.getByRole("dialog");
    await expect(dialog).toContainText("Add a credential?");
    await expect(dialog).toContainText("Diploma");
    await expect(window.getByText("No credentials yet")).toBeVisible();

    await dialog.getByRole("button", { name: "Add" }).click();
    const card = window.getByRole("region", { name: "Credentials" });
    await expect(card).toContainText("Diploma");
    await expect(card).toContainText("ada@example.com");
    await expect(card).toContainText("Mathematics");
    await window.screenshot({ path: process.env.ZKPUOI_E2E_SCREENSHOT });
  });

  test("adds nothing when the user declines an offer", async () => {
    const offer = await issuer.createOffer({ name: "Grace Hopper", email: "grace@example.com", degree: "Computer Science", university: "Trusted University" });

    await openLink(offer);
    await window.getByRole("dialog").getByRole("button", { name: "Decline" }).click();

    await expect(window.getByRole("region", { name: "Credentials" }).locator("article")).toHaveCount(1);
  });

  test("answers a presentation request with only what was asked, after the user agrees", async () => {
    const request = new URLSearchParams({
      response_type: "vp_token",
      client_id: `redirect_uri:${responseUri}`,
      response_uri: responseUri,
      response_mode: "direct_post",
      nonce: "n-1",
      state: "s-1",
      dcql_query: JSON.stringify({
        credentials: [{ id: "credential", format: "dc+sd-jwt", meta: { vct_values: ["Diploma"] }, claims: [{ path: ["degree"] }, { path: ["email"] }] }],
      }),
      client_metadata: JSON.stringify({
        vp_formats_supported: { "dc+sd-jwt": { "sd-jwt_alg_values": ["EdDSA", "ES256"], "kb-jwt_alg_values": ["EdDSA", "ES256"] } },
      }),
    });

    await openLink(`openid4vp://?${request}`);
    const dialog = window.getByRole("dialog");
    await expect(dialog).toContainText("Share information?");
    await expect(dialog).toContainText(responseUri);
    await expect(dialog).toContainText("degree");
    await expect(dialog).toContainText("email");
    expect(answers).toHaveLength(0);

    await dialog.getByRole("button", { name: "Share" }).click();
    await expect(window.getByRole("status")).toContainText("Your answer was sent");

    expect(answers).toHaveLength(1);
    expect(answers[0].get("state")).toBe("s-1");
    const vpToken = JSON.parse(answers[0].get("vp_token") ?? "{}") as { credential: string[] };
    expect(vpToken.credential).toHaveLength(1);
  });

  test("will not share when it holds nothing that answers the request", async () => {
    const request = new URLSearchParams({
      response_type: "vp_token",
      client_id: `redirect_uri:${responseUri}`,
      response_uri: responseUri,
      response_mode: "direct_post",
      nonce: "n-2",
      state: "s-2",
      dcql_query: JSON.stringify({ credentials: [{ id: "credential", format: "dc+sd-jwt", meta: { vct_values: ["Passport"] }, claims: [{ path: ["number"] }] }] }),
    });

    await openLink(`openid4vp://?${request}`);
    const dialog = window.getByRole("dialog");
    await expect(dialog).toContainText("You hold no credential that answers this request");
    await expect(dialog.getByRole("button", { name: "Share" })).toBeDisabled();
    await dialog.getByRole("button", { name: "Decline" }).click();
  });
});

test("starts with its data in a folder whose path has spaces, as the system app folders do", async () => {
  const app = await electron.launch({
    args: [appDir],
    env: {
      ...process.env,
      ZKPUOI_WALLET_DATA_DIR: join(mkdtempSync(join(tmpdir(), "zk-puoi-wallet-")), "Application Support", "zk-puoi wallet"),
      ZKPUOI_WALLET_KEY: "e2e-wallet-key-3",
    },
  });
  try {
    await expect((await app.firstWindow()).getByText("No credentials yet")).toBeVisible();
  } finally {
    await app.close();
  }
});

test("opens a link handed to the app when it starts", async () => {
  const issuerPort = await freePort();
  const issuer = await startIssuer({ port: issuerPort, publicUrl: `http://localhost:${issuerPort}`, storeKey: "e2e-2", allowInsecureHttp: true });
  const offer = await issuer.createOffer({ name: "Ada Lovelace", email: "ada@example.com", degree: "Mathematics", university: "Trusted University" });
  const app = await electron.launch({
    args: [appDir, offer],
    env: {
      ...process.env,
      ZKPUOI_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "zk-puoi-wallet-")),
      ZKPUOI_WALLET_KEY: "e2e-wallet-key-2",
      ZKPUOI_ALLOW_INSECURE_HTTP: "1",
    },
  });
  try {
    const window = await app.firstWindow();
    await expect(window.getByRole("dialog")).toContainText("Add a credential?");
  } finally {
    await app.close();
    await issuer.close();
  }
});
