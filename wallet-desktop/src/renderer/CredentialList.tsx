import type { HeldCredential } from "../shared/api.js";

// Fields that belong to the credential's mechanics, not to what it says about the holder.
const technical = new Set(["iss", "iat", "exp", "nbf", "vct", "cnf", "_sd_alg", "status"]);

export function CredentialList({ credentials }: { credentials: HeldCredential[] }) {
  if (credentials.length === 0) {
    return <p className="empty">Nog geen credentials. Open een aanbod van een uitgever om er een toe te voegen.</p>;
  }
  return (
    <section aria-label="Credentials">
      {credentials.map((credential) => (
        <article key={credential.id} className="credential">
          <h2>{credential.type}</h2>
          <p className="issuer">Uitgegeven door {credential.issuer}</p>
          <dl>
            {Object.entries(credential.claims)
              .filter(([name]) => !technical.has(name))
              .map(([name, value]) => (
                <div key={name}>
                  <dt>{name}</dt>
                  <dd>{String(value)}</dd>
                </div>
              ))}
          </dl>
        </article>
      ))}
    </section>
  );
}
