// How the end-to-end tests get a credential into the real app: the way a holder does, with a login at the issuer. The browser is played in code.
import { expect, type ElectronApplication, type Page } from "@playwright/test";
import { browserLogin, requestLink, type TestIssuer } from "../../examples/issuer/api/src/testing.js";

type WithOpened = { opened?: string[] };

/** The app does not open its browser; the addresses it would open are kept, so a test can follow them. */
export async function captureBrowser(app: ElectronApplication): Promise<void> {
  await app.evaluate(({ shell }) => {
    const opened: string[] = [];
    (globalThis as WithOpened).opened = opened;
    shell.openExternal = async (url: string) => void opened.push(url);
  });
}

export const openedByApp = (app: ElectronApplication): Promise<string[]> => app.evaluate(() => (globalThis as WithOpened).opened ?? []);

/** What the system does when a link is opened: it hands the link to the app. */
export const handOver = (app: ElectronApplication, link: string): Promise<void> =>
  app.evaluate(({ app: electronApp }, l) => void electronApp.emit("open-url", { preventDefault() {} }, l), link);

const goTo = (window: Page, page: string) => window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: page, exact: true }).click();

export async function openLink(window: Page, link: string): Promise<void> {
  await goTo(window, "Link openen");
  await window.getByLabel("Link").fill(link);
  await window.getByRole("button", { name: "Openen" }).click();
}

/** The holder asks for the credential in the app and logs in at the issuer as `email`; resolves once the app says the credential is added. */
export async function addCredential(app: ElectronApplication, window: Page, issuer: TestIssuer, type: string, email: string): Promise<void> {
  await openLink(window, requestLink(issuer.url, type));
  await window.getByRole("dialog").getByRole("button", { name: "Aanmelden" }).click();
  await expect(window.getByRole("status")).toContainText("Meld u aan in uw browser");
  const address = (await openedByApp(app)).at(-1)!;
  await handOver(app, await browserLogin(address, email, issuer.mails));
  await expect(window.getByRole("status")).toContainText("toegevoegd");
}
