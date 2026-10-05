import { useState } from "react";
import type { Pending } from "../shared/api.js";

interface Props {
  pending: Pending;
  busy: boolean;
  onApprove: (remember: boolean) => void;
  onDecline: () => void;
}

export function Consent({ pending, busy, onApprove, onDecline }: Props) {
  const [remember, setRemember] = useState(false);
  const signIn = pending.kind === "presentation" && pending.signIn;
  return (
    <div className="backdrop">
      <div role="dialog" aria-modal="true" aria-label="Confirm" className="dialog">
        {pending.kind === "offer" ? (
          <>
            <h2>Add a credential?</h2>
            <p>
              <strong>{pending.issuer}</strong> offers you:
            </p>
            <ul>
              {pending.types.map((type) => (
                <li key={type}>{type}</li>
              ))}
            </ul>
          </>
        ) : signIn ? (
          <>
            <h2>Aanmelden?</h2>
            <p>
              Aanmelden bij <strong>{pending.application ?? pending.verifier}</strong>
              {pending.identity && (
                <>
                  <br />
                  E-mailadres: {pending.identity.email}
                  <br />
                  DID: {pending.identity.issuer}
                </>
              )}
            </p>
            {!pending.satisfiable && <p className="error">U hebt geen bewijs dat hierbij past.</p>}
            {pending.satisfiable && pending.application && pending.identity && (
              <label className="remember">
                <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
                Altijd aanmelden, zonder de knop Aanmelden
              </label>
            )}
          </>
        ) : (
          <>
            <h2>Share information?</h2>
            <p>
              <strong>{pending.verifier}</strong> asks for:
            </p>
            {pending.requested.map((request, i) => (
              <div key={i}>
                <p>A {request.type.join(" or ") || "credential"}, showing:</p>
                <ul>
                  <li>who issued it</li>
                  {request.claims.map((claim) => (
                    <li key={claim}>{claim}</li>
                  ))}
                </ul>
              </div>
            ))}
            {!pending.satisfiable && <p className="error">You hold no credential that answers this request.</p>}
            {pending.satisfiable && pending.application && (
              <label className="remember">
                <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
                Always share with {pending.application}
              </label>
            )}
          </>
        )}
        <div className="buttons">
          <button type="button" onClick={onDecline} disabled={busy}>
            {signIn ? "Annuleren" : "Decline"}
          </button>
          <button type="button" onClick={() => onApprove(remember)} disabled={busy || (pending.kind === "presentation" && !pending.satisfiable)}>
            {pending.kind === "offer" ? "Add" : signIn ? "Bevestigen" : "Share"}
          </button>
        </div>
      </div>
    </div>
  );
}
