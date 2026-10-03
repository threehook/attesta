import { useState } from "react";
import type { Credential } from "@zk-puoi/client";
import { wallet } from "../lib/clients.js";

interface Props {
  credentials: Credential[];
  onChange: () => void;
}

// WalletView manages the mock Verifiable Credential store: plain JSON in browser storage (client-lib's Wallet), not real signed VCs — see
// client-lib/src/wallet.ts's doc comment for why.
export function WalletView({ credentials, onChange }: Props) {
  const [type, setType] = useState("Diploma");
  const [issuer, setIssuer] = useState("trusted-university");
  const [subject, setSubject] = useState("alice");

  function handleAdd(e: React.FormEvent) {
    e.preventDefault();
    wallet.add({ id: crypto.randomUUID(), type, issuer, subject, claims: {} });
    onChange();
  }

  function handleRemove(id: string) {
    wallet.remove(id);
    onChange();
  }

  return (
    <section>
      <form onSubmit={handleAdd}>
        <fieldset>
          <label>
            Credential type
            <input value={type} onChange={(e) => setType(e.target.value)} required />
          </label>
          <label>
            Issuer
            <input value={issuer} onChange={(e) => setIssuer(e.target.value)} required />
          </label>
          <label>
            Subject
            <input value={subject} onChange={(e) => setSubject(e.target.value)} required />
          </label>
        </fieldset>
        <button type="submit">Add credential</button>
      </form>

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
