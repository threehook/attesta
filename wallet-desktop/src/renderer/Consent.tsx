import type { Pending } from "../shared/api.js";

interface Props {
  pending: Pending;
  busy: boolean;
  onApprove: () => void;
  onDecline: () => void;
}

export function Consent({ pending, busy, onApprove, onDecline }: Props) {
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
          </>
        )}
        <div className="buttons">
          <button type="button" onClick={onDecline} disabled={busy}>
            Decline
          </button>
          <button type="button" onClick={onApprove} disabled={busy || (pending.kind === "presentation" && !pending.satisfiable)}>
            {pending.kind === "offer" ? "Add" : "Share"}
          </button>
        </div>
      </div>
    </div>
  );
}
