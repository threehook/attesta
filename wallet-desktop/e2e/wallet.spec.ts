// Runs the real Electron app against a real issuer and a stand-in verifier, and drives its window like a user would.
import { mkdtempSync, writeFileSync } from "node:fs";
import { createServer, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { _electron as electron, expect, test, type ElectronApplication, type Page } from "@playwright/test";
import { startTestIssuer, type TestIssuer } from "./issuer.js";
import { addCredential, uploadQr } from "./issuing.js";

const appDir = join(import.meta.dirname, "..");

const showCredentials = (page: Page) => page.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: "Credentials", exact: true }).click();

async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, resolve));
  const { port } = server.address() as AddressInfo;
  await new Promise((resolve) => server.close(resolve));
  return port;
}

test.describe.serial("desktop wallet", () => {
  let test_: TestIssuer;
  let verifier: Server;
  let responseUri: string;
  const answers: URLSearchParams[] = [];
  let app: ElectronApplication;
  let window: Page;
  const dataDir = mkdtempSync(join(tmpdir(), "attesta-wallet-"));
  let identityPort: number;

  test.beforeAll(async () => {
    test_ = await startTestIssuer();

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
    await test_?.close();
  });

  // A request for the diploma credential the wallet holds by now, answered to the stand-in verifier.
  const diplomaRequest = (state: string, vct = "Diploma Laadpalen Management", claims = ["name", "email"]) =>
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

  // The wallet's menu: each part of the wallet is a page of its own.
  const goTo = (page: string) => window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: page, exact: true }).click();

  const openLink = async (link: string) => {
    await goTo("Link openen");
    await window.getByLabel("Link").fill(link);
    await window.getByRole("button", { name: "Openen" }).click();
  };

  test("starts empty", async () => {
    await goTo("Credentials");
    await expect(window.getByText("Nog geen credentials")).toBeVisible();
  });

  test("rejects a link it does not understand", async () => {
    await openLink("https://example.com");
    await expect(window.getByRole("status")).toContainText("geen credential-link");
  });

  test("shows what a QR code gives, and adds the credential only once the user agrees", async () => {
    await test_.recordDiploma({ name: "Ada Lovelace", email: "ada@example.com" });

    await uploadQr(window, await test_.qrImageFor("ada@example.com"));
    const dialog = window.getByRole("dialog");
    await expect(dialog).toContainText("Credential toevoegen?");
    await expect(dialog).toContainText("Diploma Laadpalen Management");
    await expect(window.locator("footer")).not.toContainText("credential");

    await dialog.getByRole("button", { name: "Toevoegen" }).click();
    await goTo("Credentials");
    const card = window.getByRole("region", { name: "Credentials" });
    await expect(card.getByRole("heading", { name: "Diploma Laadpalen Management" })).toBeVisible();
    await expect(card).toContainText("ada@example.com");
    await expect(card).toContainText("Naam");
    await expect(card).toContainText("Ada Lovelace");
    await expect(card).toContainText("Geldig tot");
    await window.screenshot({ path: process.env.ATTESTA_E2E_SCREENSHOT });
  });

  test("adds nothing when the user declines", async () => {
    await test_.recordEmployee({ name: "Grace Hopper", email: "grace@example.com", department: "burgerzaken", organisation: "Gemeente Vlierdam" });

    await uploadQr(window, await test_.qrImageFor("grace@example.com"));
    await window.getByRole("dialog").getByRole("button", { name: "Weigeren" }).click();

    await goTo("Credentials");
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
        credentials: [{ id: "credential", format: "dc+sd-jwt", meta: { vct_values: ["Diploma Laadpalen Management"] }, claims: [{ path: ["name"] }, { path: ["email"] }] }],
      }),
      client_metadata: JSON.stringify({
        vp_formats_supported: { "dc+sd-jwt": { "sd-jwt_alg_values": ["EdDSA", "ES256"], "kb-jwt_alg_values": ["EdDSA", "ES256"] } },
      }),
    });

    await openLink(`openid4vp://?${request}`);
    const dialog = window.getByRole("dialog");
    await expect(dialog).toContainText("Informatie delen?");
    await expect(dialog).toContainText(responseUri);
    await expect(dialog).toContainText("Naam");
    await expect(dialog).toContainText("E-mailadres");
    expect(answers).toHaveLength(0);

    await dialog.getByRole("button", { name: "Delen", exact: true }).click();
    await expect(window.getByRole("status")).toContainText("Uw antwoord is verstuurd");

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
    await expect(dialog).toContainText("U hebt geen credential dat op dit verzoek past");
    await expect(dialog.getByRole("button", { name: "Delen", exact: true })).toBeDisabled();
    await dialog.getByRole("button", { name: "Weigeren" }).click();
  });

  test("shares without asking with an application the user chose to always share with, until it is removed", async () => {
    const application = new URL(responseUri).origin;
    const before = answers.length;

    await openLink(diplomaRequest("s-3"));
    const dialog = window.getByRole("dialog");
    await dialog.getByLabel(`Altijd delen met ${application}`).check();
    await dialog.getByRole("button", { name: "Delen", exact: true }).click();
    await expect(window.getByRole("status")).toContainText("Uw antwoord is verstuurd");
    expect(answers).toHaveLength(before + 1);

    await openLink(diplomaRequest("s-4"));
    await expect(window.getByRole("status")).toContainText(`Gedeeld: Naam, E-mailadres met redirect_uri:${responseUri}`);
    await expect(window.getByRole("dialog")).toHaveCount(0);
    expect(answers).toHaveLength(before + 2);
    expect(answers[before + 1].get("state")).toBe("s-4");

    await goTo("Delen");
    const sharing = window.getByRole("region", { name: "Delen" });
    await expect(sharing).toContainText(application);
    await sharing.getByRole("button", { name: "Verwijderen" }).click();
    await expect(sharing).not.toContainText(application);

    await openLink(diplomaRequest("s-5"));
    await expect(window.getByRole("dialog")).toContainText("Informatie delen?");
    expect(answers).toHaveLength(before + 2);
    await window.getByRole("dialog").getByRole("button", { name: "Weigeren" }).click();
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
    expect(first.detail).toBe("• wie het heeft uitgegeven\n• Naam\n• E-mailadres");
    expect(first.buttons).toEqual(["Delen", "Altijd delen", "Weigeren"]);
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

    await goTo("Delen");
    const sharing = window.getByRole("region", { name: "Delen" });
    await sharing.getByRole("button", { name: "Verwijderen" }).click();
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
    const signIn = (state: string) => diplomaRequest(state, "Diploma Laadpalen Management", ["email"]);

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
    expect((await popups())[0].buttons).toEqual(["Delen", "Altijd delen", "Weigeren"]);
    expect(answers).toHaveLength(before + 3);

    await goTo("Delen");
    const sharing = window.getByRole("region", { name: "Delen" });
    await expect(sharing).toContainText(application);
    await sharing.getByRole("button", { name: "Verwijderen" }).click();
    await expect(sharing).not.toContainText(application);
  });

  test("tells a page which identities the user has, and signs in with the one the link names", async () => {
    const application = new URL(responseUri).origin;
    const before = answers.length;
    const res = await fetch(`http://127.0.0.1:${identityPort}/identities?type=Diploma Laadpalen Management&application=${encodeURIComponent(application)}`, {
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
      `${diplomaRequest(state, "Diploma Laadpalen Management", ["email"])}&${new URLSearchParams({ login_hint: email, issuer_hint: issuer })}`;

    await answerPopupsWith(0, true);
    await handOver(named("h-1", identity.email, identity.issuer));
    await expect.poll(() => answers.length).toBe(before + 1);
    const [popup] = await popups();
    expect(popup.message).toBe(`Aanmelden bij ${application}?`);
    expect(popup.detail).toBe(`E-mailadres: ${identity.email}\nDID: ${identity.issuer}`);

    // The wallet remembered the identity, and tells the page so.
    const again = await fetch(`http://127.0.0.1:${identityPort}/identities?type=Diploma Laadpalen Management&application=${encodeURIComponent(application)}`);
    expect(((await again.json()) as { identities: Array<{ automatic: boolean }> }).identities[0].automatic).toBe(true);
    await answerPopupsWith(1, false);
    await handOver(named("h-2", identity.email, identity.issuer));
    await expect.poll(() => answers.length).toBe(before + 2);
    expect(await popups()).toHaveLength(0);

    // An identity the wallet does not hold is not answered with another.
    await handOver(named("h-3", "nobody@example.com", identity.issuer));
    await expect.poll(async () => (await popups()).length).toBe(1);
    expect(answers).toHaveLength(before + 2);

    await goTo("Delen");
    const sharing = window.getByRole("region", { name: "Delen" });
    await expect(sharing).toContainText(identity.email);
    await sharing.getByRole("button", { name: "Verwijderen" }).click();
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
    const window = await app.firstWindow();
    await showCredentials(window);
    await expect(window.getByText("Nog geen credentials")).toBeVisible();
  } finally {
    await app.close();
  }
});

test("opens a link handed to the app when it starts", async () => {
  const issuer = await startTestIssuer();
  await issuer.recordDiploma({ name: "Ada Lovelace", email: "ada@example.com" });
  const link = await issuer.qrLinkFor("ada@example.com");
  const app = await electron.launch({
    args: [appDir, link],
    env: {
      ...process.env,
      ATTESTA_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "attesta-wallet-")),
      ATTESTA_WALLET_KEY: "e2e-wallet-key-2",
      ATTESTA_ALLOW_INSECURE_HTTP: "1",
    },
  });
  try {
    const window = await app.firstWindow();
    await expect(window.getByRole("dialog")).toContainText("Credential toevoegen?");
  } finally {
    await app.close();
    await issuer.close();
  }
});

test("opens a new window for a link that arrives after its window was closed", async () => {
  test.skip(process.platform !== "darwin", "only macOS keeps the app running without a window");
  const issuer = await startTestIssuer();
  await issuer.recordDiploma({ name: "Ada Lovelace", email: "ada@example.com" });
  const link = await issuer.qrLinkFor("ada@example.com");
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
    await showCredentials(first);
    await expect(first.getByText("Nog geen credentials")).toBeVisible();
    await first.close();
    // What macOS sends when a link is opened while the app runs without a window.
    await app.evaluate(({ app: electronApp }, link) => electronApp.emit("open-url", { preventDefault() {} }, link), link);
    const next = await app.waitForEvent("window");
    await expect(next.getByRole("dialog")).toContainText("Credential toevoegen?");
  } finally {
    await app.close();
    await issuer.close();
  }
});

// The QR code in the mail, uploaded in the wallet: what the app does when the code is good, expired, withdrawn or not a code at all.
test.describe.serial("desktop wallet, a QR code from a mail", () => {
  let issuer: TestIssuer;
  let app: ElectronApplication;
  let window: Page;
  const employee = {
    name: "Jerry Smith",
    email: "jerry@example.com",
    department: "burgerzaken" as const,
    organisation: "Gemeente Vlierdam",
    roles: ["laadpalen-aanvrager", "reader"],
  };

  test.beforeAll(async () => {
    issuer = await startTestIssuer({ qrLifetimeSeconds: 2, renewalCooldown: "500ms" });
    app = await electron.launch({
      args: [appDir],
      env: {
        ...process.env,
        ATTESTA_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "attesta-wallet-")),
        ATTESTA_WALLET_KEY: "e2e-wallet-key-qr",
        ATTESTA_ALLOW_INSECURE_HTTP: "1",
      },
    });
    window = await app.firstWindow();
  });

  test.afterAll(async () => {
    await app?.close();
    await issuer?.close();
  });

  const credentials = () => window.getByRole("region", { name: "Credentials" }).locator("article");
  const goTo = (page: string) => window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: page, exact: true }).click();

  test("says so when the credential was withdrawn after the mail", async () => {
    const id = await issuer.recordEmployee({ ...employee, email: "henk@example.com", name: "Henk" });
    await issuer.revoke(id);

    await uploadQr(window, await issuer.qrImageFor("henk@example.com"));
    await window.getByRole("dialog").getByRole("button", { name: "Toevoegen" }).click();

    await expect(window.getByRole("status")).toContainText("Er staat geen credential voor u klaar");
    await goTo("Credentials");
    await expect(credentials()).toHaveCount(0);
  });

  test("adds an employee credential from the uploaded QR code", async () => {
    await issuer.recordEmployee(employee);

    await addCredential(window, issuer, employee.email);

    await goTo("Credentials");
    const card = window.getByRole("region", { name: "Credentials" });
    await expect(card.getByRole("heading", { name: "Medewerker afdeling & rollen" })).toBeVisible();
    await expect(card).toContainText("jerry@example.com");
    await expect(card).toContainText("Deze credential is verplicht om werkzaamheden te mogen verrichten voor onze gemeente");
    await expect(card).toContainText("Rollen");
    await expect(card).toContainText("laadpalen-aanvrager, reader");
    await expect(card).not.toContainText("Geldig tot");
    await expect(card).toContainText("burgerzaken");
  });

  test("says so when the image has no QR code", async () => {
    await uploadQr(window, Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64"), "blank.png");

    await expect(window.getByRole("status")).toContainText("geen QR-code");
    await expect(window.getByRole("dialog")).toHaveCount(0);
  });

  test("says so when the file is not an image", async () => {
    await uploadQr(window, Buffer.from("not an image"), "notes.txt");

    await expect(window.getByRole("status")).toContainText("geen PNG- of JPEG");
  });

  test("refuses a credential of a type it already holds from that issuer, and the QR code stays usable", async () => {
    await issuer.recordEmployee({ ...employee, email: "grace@example.com", name: "Grace Hopper" });

    await uploadQr(window, await issuer.qrImageFor("grace@example.com"));

    await expect(window.getByRole("status")).toContainText("U heeft al een credential \"Medewerker afdeling & rollen\"");
    await expect(window.getByRole("dialog")).toHaveCount(0);
  });

  test("removes a credential after the user confirms, and then accepts the QR code that was refused", async () => {
    await goTo("Credentials");
    await credentials().getByRole("button", { name: "Verwijderen" }).click();
    await credentials().getByRole("button", { name: "Annuleren" }).click();
    await expect(credentials()).toHaveCount(1);

    await credentials().getByRole("button", { name: "Verwijderen" }).click();
    await credentials().getByRole("button", { name: "Ja, verwijderen" }).click();
    await expect(window.getByRole("status")).toContainText("Credential verwijderd");
    await expect(credentials()).toHaveCount(0);

    await addCredential(window, issuer, "grace@example.com");
    await goTo("Credentials");
    await expect(credentials()).toHaveCount(1);
    await expect(credentials().first()).toContainText("grace@example.com");
  });

  test("tells the user that an expired QR code was replaced, and the new one works", async () => {
    await issuer.recordDiploma({ email: "hopper@example.com", name: "Grace Hopper" });
    const mails = await issuer.mailCount();
    const old = await issuer.qrImageFor("hopper@example.com");
    await new Promise((resolve) => setTimeout(resolve, 2500));

    await uploadQr(window, old);

    await expect(window.getByRole("status")).toContainText("De QR-code is verlopen. Er is een nieuwe QR-code naar uw e-mail gestuurd.");
    expect(await issuer.mailCount()).toBe(mails + 1);
    await uploadQr(window, await issuer.qrImageFor("hopper@example.com"));
    await window.getByRole("dialog").getByRole("button", { name: "Toevoegen" }).click();
    await expect(window.getByRole("status")).toContainText("1 credential(s) toegevoegd");
    await goTo("Credentials");
    await expect(credentials()).toHaveCount(2);
  });
});
