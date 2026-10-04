import { useCallback, useEffect, useState } from "react";
import type { HeldCredential, Pending } from "../shared/api.js";
import { Consent } from "./Consent.js";
import { CredentialList } from "./CredentialList.js";

type Notice = { kind: "info" | "error"; text: string };

export function App() {
  const [credentials, setCredentials] = useState<HeldCredential[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [link, setLink] = useState("");
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    const result = await window.wallet.list();
    if (result.ok) setCredentials(result.value);
    else setNotice({ kind: "error", text: result.error });
  }, []);

  const open = useCallback(async (value: string) => {
    setNotice(null);
    setBusy(true);
    try {
      const result = await window.wallet.prepare(value);
      if (result.ok) setPending(result.value);
      else setNotice({ kind: "error", text: result.error });
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    window.wallet.onLink((incoming) => void open(incoming));
  }, [refresh, open]);

  async function approve() {
    if (!pending) return;
    setBusy(true);
    try {
      const result = await window.wallet.approve(pending.id);
      setPending(null);
      if (!result.ok) {
        setNotice({ kind: "error", text: result.error });
      } else if (result.value.kind === "offer") {
        setNotice({ kind: "info", text: `Added ${result.value.credentials.length} credential(s) to your wallet.` });
        await refresh();
      } else {
        setNotice({ kind: "info", text: "Your answer was sent." });
      }
    } finally {
      setBusy(false);
    }
  }

  async function decline() {
    if (pending) await window.wallet.decline(pending.id);
    setPending(null);
  }

  function submit(event: React.FormEvent) {
    event.preventDefault();
    if (link.trim()) {
      void open(link);
      setLink("");
    }
  }

  return (
    <main>
      <h1>Wallet</h1>

      <form onSubmit={submit}>
        <label>
          Open a link
          <input
            value={link}
            onChange={(e) => setLink(e.target.value)}
            placeholder="openid-credential-offer://… or openid4vp://…"
            aria-label="Link"
          />
        </label>
        <button type="submit" disabled={busy}>
          Open
        </button>
      </form>

      {notice && (
        <p role="status" className={notice.kind}>
          {notice.text}
        </p>
      )}

      <CredentialList credentials={credentials} />

      {pending && <Consent pending={pending} busy={busy} onApprove={approve} onDecline={decline} />}
    </main>
  );
}
