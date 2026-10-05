import { useCallback, useEffect, useState } from "react";
import type { HeldCredential, Pending } from "../shared/api.js";
import { Consent } from "./Consent.js";
import { CredentialList } from "./CredentialList.js";
import { SharingSettings } from "./SharingSettings.js";

type Notice = { kind: "info" | "error"; text: string };

export function App() {
  const [credentials, setCredentials] = useState<HeldCredential[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [link, setLink] = useState("");
  const [busy, setBusy] = useState(false);
  const [settingsVersion, setSettingsVersion] = useState(0);

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
      if (!result.ok) setNotice({ kind: "error", text: result.error });
      else if (result.value.kind === "shared") setNotice({ kind: "info", text: `Shared ${result.value.claims.join(", ")} with ${result.value.verifier}.` });
      else setPending(result.value);
    } finally {
      setBusy(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
    window.wallet.onSettingsChanged(() => setSettingsVersion((v) => v + 1));
    window.wallet.onLink((incoming) => {
      if (incoming.ok) setPending(incoming.value);
      else setNotice({ kind: "error", text: incoming.error });
    });
  }, [refresh]);

  async function approve(remember: boolean) {
    if (!pending) return;
    setBusy(true);
    try {
      const result = await window.wallet.approve(pending.id, remember);
      setPending(null);
      setSettingsVersion((v) => v + 1);
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

      <SharingSettings version={settingsVersion} />

      {pending && <Consent pending={pending} busy={busy} onApprove={approve} onDecline={decline} />}
    </main>
  );
}
