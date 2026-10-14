// How the end-to-end tests get a credential into the real app: the way a holder does, with the QR code from the mail uploaded in the wallet.
import { expect, type Page } from "@playwright/test";
import type { TestIssuer } from "./issuer.js";

const goTo = (window: Page, page: string) => window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: page, exact: true }).click();

/** The holder chooses an image on the wallet's upload page. */
export async function uploadQr(window: Page, image: Buffer, name = "credential.png"): Promise<void> {
  await goTo(window, "QR-code uploaden");
  await window.getByLabel("QR-code").setInputFiles({ name, mimeType: name.endsWith(".png") ? "image/png" : "application/octet-stream", buffer: image });
}

/** The holder uploads the QR code from the newest mail to this person and agrees. */
export async function addCredential(window: Page, issuer: TestIssuer, email: string): Promise<void> {
  await uploadQr(window, await issuer.qrImageFor(email));
  await window.getByRole("dialog").getByRole("button", { name: "Toevoegen" }).click();
  await expect(window.getByRole("status")).toContainText("toegevoegd");
}
