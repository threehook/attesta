import { useState } from "react";
import type { AuthorizationRequestResponse, Subject } from "@attesta/client";
import { apiClient } from "../lib/clients.js";

type Decision = { allow: boolean; reason: string; subject?: Subject };

// ResourceView is the relying application: it asks the backend to start an authorization, hands the wallet link to the user, and waits for the
// decision. The wallet answers the backend directly; this page only learns the outcome.
export function ResourceView() {
  const [resource, setResource] = useState("diploma-vault");
  const [credentialType, setCredentialType] = useState("Diploma");
  const [claims, setClaims] = useState("degree");
  const [pending, setPending] = useState(false);
  const [request, setRequest] = useState<AuthorizationRequestResponse | null>(null);
  const [decision, setDecision] = useState<Decision | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setDecision(null);
    setRequest(null);
    setPending(true);
    try {
      const created = await apiClient.createAuthorizationRequest({
        resource,
        policyId: "diploma_check",
        credentialType,
        claims: claims
          .split(",")
          .map((c) => c.trim())
          .filter(Boolean),
      });
      setRequest(created);
      const outcome = await apiClient.waitForOutcome(created.requestId);
      setDecision({ allow: outcome.allow, reason: outcome.reason, subject: outcome.subject });
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
            Bron
            <input value={resource} onChange={(e) => setResource(e.target.value)} required />
          </label>
          <label>
            Soort bewijs
            <input value={credentialType} onChange={(e) => setCredentialType(e.target.value)} required />
          </label>
          <label>
            Te delen gegevens naast het e-mailadres (kommagescheiden)
            <input value={claims} onChange={(e) => setClaims(e.target.value)} />
          </label>
        </fieldset>
        <button type="submit" disabled={pending}>
          {pending ? "Wachten op de wallet…" : "Toegang vragen"}
        </button>
      </form>

      {request && !decision && (
        <p>
          Open deze link in uw wallet: <a href={request.authorizationRequest}>open in wallet</a>
          <br />
          <code>{request.authorizationRequest}</code>
        </p>
      )}

      {error && <p className="error">{error}</p>}

      {decision && (
        <div className={`result ${decision.allow ? "allow" : "deny"}`}>
          <strong>{decision.allow ? "Toegestaan" : "Geweigerd"}</strong>: {decision.reason}
          {decision.subject && (
            <p>
              Aangemeld als <strong>{decision.subject.email}</strong> (bevestigd door <code>{decision.subject.issuer}</code>)
            </p>
          )}
        </div>
      )}
    </section>
  );
}
