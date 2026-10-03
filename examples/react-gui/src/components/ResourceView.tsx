import { useState } from "react";
import type { AuthorizeResponse, Credential } from "@zk-puoi/client";
import { buildProof } from "@zk-puoi/client";
import { apiClient, CUBIC_WASM_PATH, CUBIC_ZKEY_PATH } from "../lib/clients.js";

interface Props {
  token: string;
  credentials: Credential[];
}

// ResourceView is the actual proof-then-authorize flow: build a real Groth16 proof in the browser for the toy cubic circuit (x^3+x+5=y), then submit
// it to /v1/authorize. The circuit takes a raw x, not a value derived from a wallet credential — there's no mapping between the two defined yet, so
// x is typed in directly to demo the end-to-end flow.
export function ResourceView({ token, credentials }: Props) {
  const [resource, setResource] = useState("diploma-vault");
  const [x, setX] = useState("3");
  const [issuer, setIssuer] = useState(credentials[0]?.issuer ?? "trusted-university");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<AuthorizeResponse | null>(null);
  const [publicY, setPublicY] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setResult(null);
    setPublicY(null);
    setPending(true);
    try {
      const { proof, publicSignals } = await buildProof(
        { x: Number(x) },
        { wasmPath: CUBIC_WASM_PATH, zkeyPath: CUBIC_ZKEY_PATH },
      );
      setPublicY(publicSignals[0] ?? null);
      const response = await apiClient.authorize({ resource, proof, publicSignals, issuer }, token);
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
            Private x (fed into the toy circuit: proves knowledge of x such that x³+x+5=y)
            <input type="number" value={x} onChange={(e) => setX(e.target.value)} required />
          </label>
          <label>
            Issuer claim (stands in for a real disclosed credential field — see client-lib README)
            <input value={issuer} onChange={(e) => setIssuer(e.target.value)} required />
          </label>
        </fieldset>
        <button type="submit" disabled={pending}>
          {pending ? "Building proof & requesting…" : "Request access"}
        </button>
      </form>

      {error && <p className="error">{error}</p>}

      {result && (
        <div className={`result ${result.allow ? "allow" : "deny"}`}>
          <strong>{result.allow ? "Allowed" : "Denied"}</strong>: {result.reason}
          {publicY && (
            <p>
              Proved: x³+x+5 = <code>{publicY}</code>, without revealing x.
            </p>
          )}
        </div>
      )}
    </section>
  );
}
