// Runs the real Electron app against a real issuer and a stand-in verifier, and drives its window like a user would.
import { mkdtempSync, writeFileSync } from "node:fs";
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
  const dataDir = mkdtempSync(join(tmpdir(), "attesta-wallet-"));
  let identityPort: number;

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

    identityPort = await freePort();
    app = await electron.launch({
      args: [appDir],
      env: {
        ...process.env,
        ATTESTA_WALLET_PORT: String(identityPort),
        ATTESTA_WALLET_DATA_DIR: dataDir,
        ATTESTA_WALLET_KEY: "e2e-wallet-key",
        ATTESTA_ALLOW_INSECURE_HTTP: "1",
      },
    });
    window = await app.firstWindow();
  });

  test.afterAll(async () => {
    await app?.close();
    verifier?.close();
    await issuer?.close();
  });

  // A request for the Diploma credential the wallet holds by now, answered to the stand-in verifier.
  const diplomaRequest = (state: string, vct = "Diploma", claims = ["degree", "email"]) =>
    `openid4vp://?${new URLSearchParams({
      response_type: "vp_token",
      client_id: `redirect_uri:${responseUri}`,
      response_uri: responseUri,
      response_mode: "direct_post",
      nonce: `n-${state}`,
      state,
      dcql_query: JSON.stringify({
        credentials: [{ id: "credential", format: "dc+sd-jwt", meta: { vct_values: [vct] }, claims: claims.map((claim) => ({ path: [claim] })) }],
      }),
      client_metadata: JSON.stringify({
        vp_formats_supported: { "dc+sd-jwt": { "sd-jwt_alg_values": ["EdDSA", "ES256"], "kb-jwt_alg_values": ["EdDSA", "ES256"] } },
      }),
    })}`;

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
    await window.screenshot({ path: process.env.ATTESTA_E2E_SCREENSHOT });
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

  test("shares without asking with an application the user chose to always share with, until it is removed", async () => {
    const application = new URL(responseUri).origin;
    const before = answers.length;

    await openLink(diplomaRequest("s-3"));
    const dialog = window.getByRole("dialog");
    await dialog.getByLabel(`Always share with ${application}`).check();
    await dialog.getByRole("button", { name: "Share" }).click();
    await expect(window.getByRole("status")).toContainText("Your answer was sent");
    expect(answers).toHaveLength(before + 1);

    await openLink(diplomaRequest("s-4"));
    await expect(window.getByRole("status")).toContainText(`Shared degree, email with redirect_uri:${responseUri}`);
    await expect(window.getByRole("dialog")).toHaveCount(0);
    expect(answers).toHaveLength(before + 2);
    expect(answers[before + 1].get("state")).toBe("s-4");

    const sharing = window.getByRole("region", { name: "Sharing" });
    await expect(sharing).toContainText(application);
    await sharing.getByRole("button", { name: "Remove" }).click();
    await expect(sharing).not.toContainText(application);

    await openLink(diplomaRequest("s-5"));
    await expect(window.getByRole("dialog")).toContainText("Share information?");
    expect(answers).toHaveLength(before + 2);
    await window.getByRole("dialog").getByRole("button", { name: "Decline" }).click();
  });

  test("asks in a popup, never in the window, for a request the system hands over", async () => {
    const application = new URL(responseUri).origin;
    const before = answers.length;
    // The system popup is replaced by one that records what it was asked and answers as a user would: 0 shares, 1 always shares, 2 declines.
    const answerPopupsWith = (response: number) =>
      app.evaluate(({ dialog }, answer) => {
        const g = globalThis as unknown as { popups: unknown[] };
        g.popups = [];
        dialog.showMessageBox = (async (options: unknown) => {
          g.popups.push(options);
          return { response: answer, checkboxChecked: false };
        }) as never;
      }, response);
    const popups = () => app.evaluate(() => (globalThis as unknown as { popups: Array<{ message: string; detail: string; buttons: string[] }> }).popups);
    const handOver = (link: string) => app.evaluate(({ app: electronApp }, l) => electronApp.emit("open-url", { preventDefault() {} }, l), link);

    await answerPopupsWith(0);
    await handOver(diplomaRequest("p-1"));
    await expect.poll(() => answers.length).toBe(before + 1);
    const [first] = await popups();
    expect(first.message).toContain(application);
    expect(first.detail).toBe("• who issued it\n• degree\n• email");
    expect(first.buttons).toEqual(["Share", "Always share", "Decline"]);
    await expect(window.getByRole("dialog")).toHaveCount(0);

    await answerPopupsWith(2);
    await handOver(diplomaRequest("p-2"));
    await expect.poll(async () => (await popups()).length).toBe(1);
    expect(answers).toHaveLength(before + 1);

    await answerPopupsWith(1);
    await handOver(diplomaRequest("p-3"));
    await expect.poll(() => answers.length).toBe(before + 2);

    await answerPopupsWith(2);
    await handOver(diplomaRequest("p-4"));
    await expect.poll(() => answers.length).toBe(before + 3);
    expect(await popups()).toHaveLength(0);
    expect(answers[before + 2].get("state")).toBe("p-4");
    await expect(window.getByRole("dialog")).toHaveCount(0);

    const sharing = window.getByRole("region", { name: "Sharing" });
    await sharing.getByRole("button", { name: "Remove" }).click();
    await expect(sharing).not.toContainText(application);

    await answerPopupsWith(0);
    await handOver(diplomaRequest("p-5", "Passport"));
    await expect.poll(async () => (await popups()).length).toBe(1);
    expect((await popups())[0].buttons).toEqual(["OK"]);
    expect(answers).toHaveLength(before + 3);
  });

  test("signs in after a popup that says so, and automatically once the user ticked the checkbox", async () => {
    const application = new URL(responseUri).origin;
    const before = answers.length;
    const answerPopupsWith = (response: number, checkboxChecked: boolean) =>
      app.evaluate(
        ({ dialog }, answer) => {
          const g = globalThis as unknown as { popups: unknown[] };
          g.popups = [];
          dialog.showMessageBox = (async (options: unknown) => {
            g.popups.push(options);
            return answer;
          }) as never;
        },
        { response, checkboxChecked },
      );
    const popups = () =>
      app.evaluate(() => (globalThis as unknown as { popups: Array<{ message: string; detail: string; buttons: string[]; checkboxLabel?: string }> }).popups);
    const handOver = (link: string) => app.evaluate(({ app: electronApp }, l) => electronApp.emit("open-url", { preventDefault() {} }, l), link);
    const signIn = (state: string) => diplomaRequest(state, "Diploma", ["email"]);

    await answerPopupsWith(0, false);
    await handOver(signIn("si-1"));
    await expect.poll(() => answers.length).toBe(before + 1);
    const [first] = await popups();
    expect(first.message).toBe(`Aanmelden bij ${application}?`);
    expect(first.detail).toContain("E-mailadres: ");
    expect(first.buttons).toEqual(["Bevestigen", "Annuleren"]);
    expect(first.checkboxLabel).toBe("Altijd aanmelden, zonder de knop Aanmelden");

    await answerPopupsWith(1, true);
    await handOver(signIn("si-2"));
    await expect.poll(async () => (await popups()).length).toBe(1);
    expect(answers).toHaveLength(before + 1);

    await answerPopupsWith(0, true);
    await handOver(signIn("si-3"));
    await expect.poll(() => answers.length).toBe(before + 2);

    await answerPopupsWith(1, false);
    await handOver(signIn("si-4"));
    await expect.poll(() => answers.length).toBe(before + 3);
    expect(await popups()).toHaveLength(0);

    // Signing in automatically does not share anything else.
    await answerPopupsWith(2, false);
    await handOver(diplomaRequest("si-5"));
    await expect.poll(async () => (await popups()).length).toBe(1);
    expect((await popups())[0].buttons).toEqual(["Share", "Always share", "Decline"]);
    expect(answers).toHaveLength(before + 3);

    const sharing = window.getByRole("region", { name: "Sharing" });
    await expect(sharing).toContainText(application);
    await sharing.getByRole("button", { name: "Remove" }).click();
    await expect(sharing).not.toContainText(application);
  });

  test("tells a page which identities the user has, and signs in with the one the link names", async () => {
    const application = new URL(responseUri).origin;
    const before = answers.length;
    const res = await fetch(`http://127.0.0.1:${identityPort}/identities?type=Diploma&application=${encodeURIComponent(application)}`, {
      headers: { Origin: "https://page.example" },
    });
    expect(res.headers.get("access-control-allow-origin")).toBe("*");
    const { identities } = (await res.json()) as { identities: Array<{ email: string; issuer: string; automatic: boolean }> };
    expect(identities).toHaveLength(1);
    const [identity] = identities;
    expect(identity.email).toContain("@");
    expect(identity.issuer).toMatch(/^did:/);
    expect(identity.automatic).toBe(false);
    expect(Object.keys(identity).sort()).toEqual(["automatic", "email", "issuer"]);

    const answerPopupsWith = (response: number, checkboxChecked: boolean) =>
      app.evaluate(
        ({ dialog }, answer) => {
          const g = globalThis as unknown as { popups: unknown[] };
          g.popups = [];
          dialog.showMessageBox = (async (options: unknown) => {
            g.popups.push(options);
            return answer;
          }) as never;
        },
        { response, checkboxChecked },
      );
    const popups = () => app.evaluate(() => (globalThis as unknown as { popups: Array<{ message: string; detail: string }> }).popups);
    const handOver = (link: string) => app.evaluate(({ app: electronApp }, l) => electronApp.emit("open-url", { preventDefault() {} }, l), link);
    const named = (state: string, email: string, issuer: string) =>
      `${diplomaRequest(state, "Diploma", ["email"])}&${new URLSearchParams({ login_hint: email, issuer_hint: issuer })}`;

    await answerPopupsWith(0, true);
    await handOver(named("h-1", identity.email, identity.issuer));
    await expect.poll(() => answers.length).toBe(before + 1);
    const [popup] = await popups();
    expect(popup.message).toBe(`Aanmelden bij ${application}?`);
    expect(popup.detail).toBe(`E-mailadres: ${identity.email}\nDID: ${identity.issuer}`);

    // The wallet remembered the identity, and tells the page so.
    const again = await fetch(`http://127.0.0.1:${identityPort}/identities?type=Diploma&application=${encodeURIComponent(application)}`);
    expect(((await again.json()) as { identities: Array<{ automatic: boolean }> }).identities[0].automatic).toBe(true);
    await answerPopupsWith(1, false);
    await handOver(named("h-2", identity.email, identity.issuer));
    await expect.poll(() => answers.length).toBe(before + 2);
    expect(await popups()).toHaveLength(0);

    // An identity the wallet does not hold is not answered with another.
    await handOver(named("h-3", "nobody@example.com", identity.issuer));
    await expect.poll(async () => (await popups()).length).toBe(1);
    expect(answers).toHaveLength(before + 2);

    const sharing = window.getByRole("region", { name: "Sharing" });
    await expect(sharing).toContainText(identity.email);
    await sharing.getByRole("button", { name: "Remove" }).click();
    await expect(sharing).not.toContainText(identity.email);
  });

  test("answers a link from an application the user chose without opening a window", async () => {
    test.skip(process.platform !== "darwin", "elsewhere the app quits when it has nothing to show");
    const before = answers.length;
    await app.close();
    writeFileSync(join(dataDir, "settings.json"), JSON.stringify({ trustedApplications: [new URL(responseUri).origin] }));

    app = await electron.launch({
      args: [appDir, diplomaRequest("s-9")],
      env: { ...process.env, ATTESTA_WALLET_DATA_DIR: dataDir, ATTESTA_WALLET_KEY: "e2e-wallet-key", ATTESTA_ALLOW_INSECURE_HTTP: "1" },
    });
    await expect.poll(() => answers.length).toBe(before + 1);
    expect(answers[before].get("state")).toBe("s-9");
    expect(app.windows()).toHaveLength(0);
  });
});

test("starts with its data in a folder whose path has spaces, as the system app folders do", async () => {
  const app = await electron.launch({
    args: [appDir],
    env: {
      ...process.env,
      ATTESTA_WALLET_DATA_DIR: join(mkdtempSync(join(tmpdir(), "attesta-wallet-")), "Application Support", "attesta wallet"),
      ATTESTA_WALLET_KEY: "e2e-wallet-key-3",
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
      ATTESTA_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "attesta-wallet-")),
      ATTESTA_WALLET_KEY: "e2e-wallet-key-2",
      ATTESTA_ALLOW_INSECURE_HTTP: "1",
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

test("opens a new window for a link that arrives after its window was closed", async () => {
  test.skip(process.platform !== "darwin", "only macOS keeps the app running without a window");
  const issuerPort = await freePort();
  const issuer = await startIssuer({ port: issuerPort, publicUrl: `http://localhost:${issuerPort}`, storeKey: "e2e-4", allowInsecureHttp: true });
  const offer = await issuer.createOffer({ name: "Ada Lovelace", email: "ada@example.com", degree: "Mathematics", university: "Trusted University" });
  const app = await electron.launch({
    args: [appDir],
    env: {
      ...process.env,
      ATTESTA_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "attesta-wallet-")),
      ATTESTA_WALLET_KEY: "e2e-wallet-key-4",
      ATTESTA_ALLOW_INSECURE_HTTP: "1",
    },
  });
  try {
    const first = await app.firstWindow();
    await expect(first.getByText("No credentials yet")).toBeVisible();
    await first.close();
    // What macOS sends when a link is opened while the app runs without a window.
    await app.evaluate(({ app: electronApp }, link) => electronApp.emit("open-url", { preventDefault() {} }, link), offer);
    const next = await app.waitForEvent("window");
    await expect(next.getByRole("dialog")).toContainText("Add a credential?");
  } finally {
    await app.close();
    await issuer.close();
  }
});
