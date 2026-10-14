// How the end-to-end tests get a credential into the real app: the way a holder does, with the QR code from the mail uploaded in the wallet.
import { expect, type Page } from "@playwright/test";
import { qrImageFor, type TestIssuer } from "../../issuer/api/src/testing.js";

const goTo = (window: Page, page: string) => window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: page, exact: true }).click();

/** The holder chooses an image on the wallet's upload page. */
export async function uploadQr(window: Page, image: Buffer, name = "credential.png"): Promise<void> {
  await goTo(window, "QR-code uploaden");
  await window.getByLabel("QR-code").setInputFiles({ name, mimeType: name.endsWith(".png") ? "image/png" : "application/octet-stream", buffer: image });
}

/** The issuer mails the person's open entitlement of this type, as it does when it is recorded; the holder uploads the QR code and agrees. */
export async function addCredential(window: Page, issuer: TestIssuer, type: string, email: string): Promise<void> {
  const entitlement = (await issuer.issuer.entitlements.findOpenByEmail(email)).find((e) => e.type === type);
  if (!entitlement) throw new Error(`there is no open ${type} entitlement for ${email}`);
  await issuer.issuer.mailQrCode(entitlement);
  await uploadQr(window, qrImageFor(issuer, email));
  await window.getByRole("dialog").getByRole("button", { name: "Toevoegen" }).click();
  await expect(window.getByRole("status")).toContainText("toegevoegd");
}
