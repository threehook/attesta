// The whole flow through the real app and the real backend: issuer -> wallet -> backend -> Gno policy. Needs a running backend whose
// ATTESTA_PUBLIC_URL is ATTESTA_BACKEND_URL (and ATTESTA_ADMIN_TOKEN for it); skipped otherwise.
import { mkdtempSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { _electron as electron, expect, test } from "@playwright/test";
import { startIssuer } from "../../examples/issuer/src/issuer.js";

const backend = process.env.ATTESTA_BACKEND_URL?.replace(/\/$/, "");
const adminToken = process.env.ATTESTA_ADMIN_TOKEN ?? "dev-only-insecure-admin-token";

async function freePort(): Promise<number> {
  const server = createServer();
  await new Promise<void>((resolve) => server.listen(0, resolve));
  const { port } = server.address() as { port: number };
  await new Promise((resolve) => server.close(resolve));
  return port;
}

const post = (path: string, body: unknown, headers: Record<string, string> = {}) =>
  fetch(`${backend}${path}`, { method: "POST", headers: { "Content-Type": "application/json", ...headers }, body: JSON.stringify(body) });

test.skip(!backend, "set ATTESTA_BACKEND_URL to run against a backend");

for (const trusted of [true, false]) {
  test(`${trusted ? "allows" : "denies"} a diploma from an issuer the policy ${trusted ? "trusts" : "does not trust"}`, async () => {
    const issuerPort = await freePort();
    const issuer = await startIssuer({ port: issuerPort, publicUrl: `http://localhost:${issuerPort}`, storeKey: "e2e-b", allowInsecureHttp: true });
    const policyId = `desktop_e2e_${trusted ? "trusted" : "untrusting"}`;
    const trustedDid = trusted ? issuer.did : "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK";
    const deployed = await post(
      "/admin/policies",
      {
        id: policyId,
        source: `package policy

func Authorize(resource string, credType string, issuer string, claims map[string]string) (bool, string) {
	if issuer != "${trustedDid}" {
		return false, "issuer not trusted: " + issuer
	}
	return true, "credential accepted for resource " + resource
}
`,
      },
      { "X-Admin-Token": adminToken },
    );
    expect(deployed.status).toBe(200);

    const app = await electron.launch({
      args: [join(import.meta.dirname, "..")],
      env: {
        ...process.env,
        ATTESTA_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "attesta-wallet-")),
        ATTESTA_WALLET_KEY: "e2e-wallet-key-b",
        ATTESTA_ALLOW_INSECURE_HTTP: "1",
      },
    });
    try {
      const window = await app.firstWindow();
      const open = async (link: string, button: string) => {
        await window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: "Link openen", exact: true }).click();
        await window.getByLabel("Link").fill(link);
        await window.getByRole("button", { name: "Openen" }).click();
        await window.getByRole("dialog").getByRole("button", { name: button }).click();
      };

      await open(await issuer.createOffer({ name: "Ada Lovelace", email: "ada@example.com", degree: "Mathematics", university: "U" }), "Toevoegen");
      await window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: "Credentials", exact: true }).click();
      await expect(window.getByRole("region", { name: "Credentials" })).toContainText("ada@example.com");

      const started = await post("/v1/authorize/requests", { resource: "diploma-vault", policyId, credentialType: "Diploma", claims: ["degree"] });
      const { requestId, authorizationRequest } = (await started.json()) as { requestId: string; authorizationRequest: string };
      await open(authorizationRequest, "Delen");
      await expect(window.getByRole("status")).toContainText("Uw antwoord is verstuurd");

      const outcome = (await (await fetch(`${backend}/v1/authorize/requests/${requestId}`)).json()) as {
        status: string;
        allow: boolean;
        reason: string;
        subject?: { issuer: string; email: string };
      };
      expect(outcome.status).toBe("done");
      expect(outcome.allow).toBe(trusted);
      if (trusted) {
        expect(outcome.subject).toEqual({ issuer: issuer.did, email: "ada@example.com" });
      } else {
        expect(outcome.reason).toContain("issuer not trusted");
        expect(outcome.subject).toBeUndefined();
      }
    } finally {
      await app.close();
      await issuer.close();
    }
  });
}
