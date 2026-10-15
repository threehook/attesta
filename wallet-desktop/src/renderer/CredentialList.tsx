import { useState } from "react";
import type { HeldCredential } from "../shared/api.js";
import { claimLabel } from "../shared/labels.js";

// Fields that belong to the credential's mechanics, not to what it says about the holder.
const technical = new Set(["iss", "iat", "exp", "nbf", "vct", "cnf", "_sd_alg", "status", "title", "validUntil"]);

// What the issuer calls the credential; the type where it gave no title.
const title = (credential: HeldCredential): string => (typeof credential.claims.title === "string" && credential.claims.title !== "" ? credential.claims.title : credential.type);

export function CredentialList({ credentials, onRemove }: { credentials: HeldCredential[]; onRemove: (id: string) => void }) {
  // The credential the user is asked to confirm removing.
  const [removing, setRemoving] = useState<string | null>(null);
  if (credentials.length === 0) {
    return <p className="empty">Nog geen credentials. Upload de QR-code uit een e-mail van een uitgever om er een toe te voegen.</p>;
  }
  return (
    <section aria-label="Credentials">
      {credentials.map((credential) => (
        <article key={credential.id} className="credential">
          <h2>{title(credential)}</h2>
          <p className="issuer">Uitgegeven door {credential.issuer}</p>
          <dl>
            {Object.entries(credential.claims)
              .filter(([name]) => !technical.has(name))
              .map(([name, value]) => (
                <div key={name}>
                  <dt>{claimLabel(name)}</dt>
                  <dd>{Array.isArray(value) ? value.join(", ") : String(value)}</dd>
                </div>
              ))}
            {typeof credential.claims.exp === "number" && (
              <div>
                <dt>Geldig tot</dt>
                <dd>{new Date(credential.claims.exp * 1000).toLocaleDateString("nl-NL", { day: "2-digit", month: "2-digit", year: "numeric", timeZone: "UTC" })}</dd>
              </div>
            )}
          </dl>
          {removing === credential.id ? (
            <p className="actions">
              Dit credential verwijderen?{" "}
              <button
                type="button"
                onClick={() => {
                  setRemoving(null);
                  onRemove(credential.id);
                }}
              >
                Ja, verwijderen
              </button>{" "}
              <button type="button" onClick={() => setRemoving(null)}>
                Annuleren
              </button>
            </p>
          ) : (
            <p className="actions">
              <button type="button" onClick={() => setRemoving(credential.id)}>
                Verwijderen
              </button>
            </p>
          )}
        </article>
      ))}
    </section>
  );
}
