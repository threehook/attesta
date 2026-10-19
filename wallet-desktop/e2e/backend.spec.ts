// The whole flow through the real app and the real backend: issuer -> wallet -> backend -> policy. Needs a running backend whose
// ATTESTA_PUBLIC_URL is ATTESTA_BACKEND_URL (and ATTESTA_ADMIN_TOKEN for it); skipped otherwise.
import { mkdtempSync, readFileSync } from "node:fs";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { _electron as electron, expect, test } from "@playwright/test";
import { startTestIssuer } from "./issuer.js";
import { addCredential } from "./issuing.js";

const backend = process.env.ATTESTA_BACKEND_URL?.replace(/\/$/, "");
const adminToken = process.env.ATTESTA_ADMIN_TOKEN ?? "dev-only-insecure-admin-token";


const post = (path: string, body: unknown, headers: Record<string, string> = {}) =>
  fetch(`${backend}${path}`, { method: "POST", headers: { "Content-Type": "application/json", ...headers }, body: JSON.stringify(body) });

test.skip(!backend, "set ATTESTA_BACKEND_URL to run against a backend");

for (const trusted of [true, false]) {
  test(`${trusted ? "allows" : "denies"} a diploma from an issuer the policy ${trusted ? "trusts" : "does not trust"}`, async () => {
    const test_ = await startTestIssuer();
    const policyId = `desktop_e2e_${trusted ? "trusted" : "untrusting"}`;
    const trustedDid = trusted ? test_.did : "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK";
    const deployed = await post(
      "/admin/policies",
      {
        id: policyId,
        source: `package policy

diploma := input.credentials["Diploma Laadpalen Management"]

decision := {"decision": "Deny", "reason": concat("", ["issuer not trusted: ", diploma.issuer])} if {
	diploma.issuer != "${trustedDid}"
} else := {"decision": "Permit", "reason": concat("", ["credential accepted for resource ", input.resource])}
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

      await test_.recordDiploma({ name: "Ada Lovelace", email: "ada@example.com" });
      await addCredential(window, test_, "ada@example.com");
      await window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: "Credentials", exact: true }).click();
      await expect(window.getByRole("region", { name: "Credentials" })).toContainText("ada@example.com");

      const started = await post("/v1/authorize/requests", { resource: "diploma-vault", policyId, credentialType: "Diploma Laadpalen Management", claims: ["name"] });
      const { requestId, authorizationRequest } = (await started.json()) as { requestId: string; authorizationRequest: string };
      await open(authorizationRequest, "Delen");
      await expect(window.getByRole("status")).toContainText("Uw antwoord is verstuurd");

      const outcome = (await (await fetch(`${backend}/v1/authorize/requests/${requestId}`)).json()) as {
        status: string;
        decision: string;
        reason: string;
        subject?: { issuer: string; email: string };
      };
      expect(outcome.status).toBe("done");
      expect(outcome.decision).toBe(trusted ? "Permit" : "Deny");
      if (trusted) {
        expect(outcome.subject).toEqual({ issuer: test_.did, email: "ada@example.com" });
      } else {
        expect(outcome.reason).toContain("issuer not trusted");
        expect(outcome.subject).toBeUndefined();
      }
    } finally {
      await app.close();
      await test_.close();
    }
  });
}

// Revoking: the backend reads the issuer's status list, so it must be started with ATTESTA_STATUS_URL_PREFIXES=http://localhost:4099/status/ and the
// tests' issuer runs on that port (ATTESTA_E2E_ISSUER_PORT=4099).
test("denies a credential after the issuer revoked it, and allows it before", async () => {
  test.skip(process.env.ATTESTA_E2E_ISSUER_PORT !== "4099", "set ATTESTA_E2E_ISSUER_PORT=4099, and start the backend as the comment above says");
  test.setTimeout(240_000);
  const test_ = await startTestIssuer();
  const trustAnyone = `package policy

decision := {"decision": "Permit", "reason": concat("", ["credential accepted for resource ", input.resource])}
`;
  expect((await post("/admin/policies", { id: "desktop_e2e_revocation", source: trustAnyone }, { "X-Admin-Token": adminToken })).status).toBe(200);

  const app = await electron.launch({
    args: [join(import.meta.dirname, "..")],
    env: {
      ...process.env,
      ATTESTA_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "attesta-wallet-")),
      ATTESTA_WALLET_KEY: "e2e-wallet-key-d",
      ATTESTA_ALLOW_INSECURE_HTTP: "1",
    },
  });
  try {
    const window = await app.firstWindow();
    const present = async () => {
      const started = await post("/v1/authorize/requests", { resource: "diploma-vault", policyId: "desktop_e2e_revocation", credentialType: "Diploma Laadpalen Management", claims: ["name"] });
      const { requestId, authorizationRequest } = (await started.json()) as { requestId: string; authorizationRequest: string };
      await window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: "Link openen", exact: true }).click();
      await window.getByLabel("Link").fill(authorizationRequest);
      await window.getByRole("button", { name: "Openen" }).click();
      await window.getByRole("dialog").getByRole("button", { name: "Delen" }).click();
      await expect(window.getByRole("status")).toContainText("Uw antwoord is verstuurd");
      return (await (await fetch(`${backend}/v1/authorize/requests/${requestId}`)).json()) as { status: string; decision: string; reason: string };
    };

    const entitlementId = await test_.recordDiploma({ name: "Ada Lovelace", email: "ada@example.com" });
    await addCredential(window, test_, "ada@example.com");

    expect(await present()).toMatchObject({ status: "done", decision: "Permit" });

    await test_.revoke(entitlementId);
    // The backend keeps the issuer's list for its ttl (a minute).
    await new Promise((resolve) => setTimeout(resolve, 62_000));

    expect(await present()).toMatchObject({ status: "done", decision: "Deny", reason: "credential has been revoked" });
  } finally {
    await app.close();
    await test_.close();
  }
});

// The laadpalen policy as it ships decides on two credentials of one person: an Employee (department, roles) and the diploma Laadpalen Management.
const laadpaalPolicy = readFileSync(join(import.meta.dirname, "../../examples/laadpalen/policies/request_laadpaal.rego"), "utf8");

const laadpaalCases = [
  { name: "an employee of Burgerzaken with the role and the diploma", roles: ["laadpalen-aanvrager"], department: "burgerzaken", decision: "Permit", reason: "Geautoriseerd" },
  { name: "an employee of another department", roles: ["laadpalen-aanvrager"], department: "secretariaat", decision: "Deny", reason: "Niet geautoriseerd vanwege afdeling" },
  { name: "an employee without the role", roles: ["reader"], department: "burgerzaken", decision: "Deny", reason: "Niet geautoriseerd vanwege rol" },
  { name: "an employee without any roles", roles: [], department: "burgerzaken", decision: "Deny", reason: "Niet geautoriseerd vanwege rol" },
];

for (const c of laadpaalCases) {
  test(`decides a laadpaal request on the Employee and the diploma together: ${c.name}`, async () => {
    const test_ = await startTestIssuer();
    const deployed = await post("/admin/policies", { id: "request_laadpaal_e2e", source: laadpaalPolicy }, { "X-Admin-Token": adminToken });
    expect(deployed.status).toBe(200);

    const app = await electron.launch({
      args: [join(import.meta.dirname, "..")],
      env: {
        ...process.env,
        ATTESTA_WALLET_DATA_DIR: mkdtempSync(join(tmpdir(), "attesta-wallet-")),
        ATTESTA_WALLET_KEY: "e2e-wallet-key-c",
        ATTESTA_ALLOW_INSECURE_HTTP: "1",
      },
    });
    try {
      const window = await app.firstWindow();
      const email = "jim.wever@example.com";
      await test_.recordEmployee({
        name: "Jim Wever",
        email,
        department: c.department,
        organisation: "Gemeente Vlierdam",
        roles: c.roles,
      });
      await addCredential(window, test_, email);
      await test_.recordDiploma({ name: "Jim Wever", email });
      await addCredential(window, test_, email);

      const started = await post("/v1/authorize/requests", {
        resource: "request_laadpaal",
        policyId: "request_laadpaal_e2e",
        credentials: [
          { credentialType: "Employee", claims: ["department", "roles"] },
          { credentialType: "Diploma Laadpalen Management" },
        ],
      });
      expect(started.status).toBe(200);
      const { requestId, authorizationRequest } = (await started.json()) as { requestId: string; authorizationRequest: string };

      await window.getByRole("navigation", { name: "Menu" }).getByRole("link", { name: "Link openen", exact: true }).click();
      await window.getByLabel("Link").fill(authorizationRequest);
      await window.getByRole("button", { name: "Openen" }).click();
      const dialog = window.getByRole("dialog");
      await expect(dialog).toContainText("Employee");
      await expect(dialog).toContainText("Diploma Laadpalen Management");
      await dialog.getByRole("button", { name: "Delen" }).click();
      await expect(window.getByRole("status")).toContainText("Uw antwoord is verstuurd");

      const outcome = (await (await fetch(`${backend}/v1/authorize/requests/${requestId}`)).json()) as {
        status: string;
        decision: string;
        reason: string;
        subject?: { email: string };
      };
      expect(outcome.status).toBe("done");
      expect({ decision: outcome.decision, reason: outcome.reason }).toEqual({ decision: c.decision, reason: c.reason });
      expect(outcome.subject?.email).toBe(c.decision === "Permit" ? email : undefined);
    } finally {
      await app.close();
      await test_.close();
    }
  });
}
