import { useState } from "react";
import type { AuthorizeResponse, Credential } from "@zk-puoi/client";
import { buildProof } from "@zk-puoi/client";
import { apiClient, CIRCUIT_WASM_PATH, CIRCUIT_ZKEY_PATH, loadRegistry, type RegistryCredentialInput } from "../lib/clients.js";

interface Props {
  token: string;
  credentials: Credential[];
}

// ResourceView is the actual proof-then-authorize flow for the diploma_membership example: build a real Groth16
// proof in the browser showing the selected wallet credential is a member of the registry and discloses the
// required type/issuer, then submit it to /v1/authorize. "diploma_check" below is this example's own policy
// choice, not something the backend assumes.
export function ResourceView({ token, credentials }: Props) {
  const [resource, setResource] = useState("diploma-vault");
  const [credentialId, setCredentialId] = useState(credentials[0]?.id ?? "");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<AuthorizeResponse | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setResult(null);
    setPending(true);
    try {
      const credential = credentials.find((c) => c.id === credentialId);
      const registryInput = credential?.claims.registryInput as RegistryCredentialInput | undefined;
      if (!registryInput) {
        throw new Error("select a credential imported from the demo registry");
      }

      const { root } = await loadRegistry();
      const { proof, publicSignals } = await buildProof(
        { ...registryInput, root, reqType: registryInput.credType, reqIssuer: registryInput.issuer },
        { wasmPath: CIRCUIT_WASM_PATH, zkeyPath: CIRCUIT_ZKEY_PATH },
      );

      const response = await apiClient.authorize({ resource, policyId: "diploma_check", proof, publicSignals }, token);
      setResult(response);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <section>
      <form onSubmit={handleSubmit}>
        <fieldset>
          <label>
            Resource
            <input value={resource} onChange={(e) => setResource(e.target.value)} required />
          </label>
          <label>
            Credential
            <select value={credentialId} onChange={(e) => setCredentialId(e.target.value)} required>
              <option value="" disabled>
                Select a credential
              </option>
              {credentials.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.type} — {c.issuer} ({c.subject})
                </option>
              ))}
            </select>
          </label>
        </fieldset>
        <button type="submit" disabled={pending || !credentialId}>
          {pending ? "Building proof & requesting…" : "Request access"}
        </button>
      </form>

      {error && <p className="error">{error}</p>}

      {result && (
        <div className={`result ${result.allow ? "allow" : "deny"}`}>
          <strong>{result.allow ? "Allowed" : "Denied"}</strong>: {result.reason}
        </div>
      )}
    </section>
  );
}
