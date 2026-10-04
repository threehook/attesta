import { useEffect, useState } from "react";
import type { Credential } from "@zk-puoi/client";
import { loadRegistry, wallet, type RegistryCredential } from "../lib/clients.js";

interface Props {
  credentials: Credential[];
  onChange: () => void;
}

// WalletView lets the user import one of a small set of pre-registered demo credentials into their wallet.
// Arbitrary typed-in credentials aren't possible here: proving membership requires a Merkle path into the real
// registry (see client-lib/circuits/diploma_membership/registry.mjs), which only exists for these few entries.
export function WalletView({ credentials, onChange }: Props) {
  const [available, setAvailable] = useState<RegistryCredential[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadRegistry()
      .then((r) => setAvailable(r.credentials))
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  function handleImport(rc: RegistryCredential) {
    wallet.add({ id: rc.id, type: rc.type, issuer: rc.issuer, subject: rc.subject, claims: { registryInput: rc.privateInput } });
    onChange();
  }

  function handleRemove(id: string) {
    wallet.remove(id);
    onChange();
  }

  const heldIds = new Set(credentials.map((c) => c.id));

  return (
    <section>
      {error && <p className="error">{error}</p>}

      <h2>Available demo credentials</h2>
      <table>
        <thead>
          <tr>
            <th>Type</th>
            <th>Issuer</th>
            <th>Subject</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {available.map((rc) => (
            <tr key={rc.id}>
              <td>{rc.type}</td>
              <td>{rc.issuer}</td>
              <td>{rc.subject}</td>
              <td>
                <button type="button" disabled={heldIds.has(rc.id)} onClick={() => handleImport(rc)}>
                  {heldIds.has(rc.id) ? "In wallet" : "Import"}
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h2>Your wallet</h2>
      {credentials.length === 0 ? (
        <p>No credentials yet.</p>
      ) : (
        <table>
          <thead>
            <tr>
              <th>Type</th>
              <th>Issuer</th>
              <th>Subject</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {credentials.map((c) => (
              <tr key={c.id}>
                <td>{c.type}</td>
                <td>{c.issuer}</td>
                <td>{c.subject}</td>
                <td>
                  <button type="button" onClick={() => handleRemove(c.id)}>
                    Remove
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}
