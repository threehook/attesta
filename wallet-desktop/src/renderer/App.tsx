import { useCallback, useEffect, useState } from "react";
import type { HeldCredential, Pending } from "../shared/api.js";
import { claimLabel } from "../shared/labels.js";
import { Consent } from "./Consent.js";
import { CredentialList } from "./CredentialList.js";
import { Blocks, Shell } from "./Layout.js";
import { HOME, MENU } from "./menu.js";
import { SharingSettings } from "./SharingSettings.js";

type Notice = { kind: "info" | "error"; text: string };

const items = MENU.flatMap((group) => group.items);
const routeOf = () => items.find((item) => item.href === window.location.hash) ?? HOME;

export function App() {
  const [credentials, setCredentials] = useState<HeldCredential[]>([]);
  const [pending, setPending] = useState<Pending | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [link, setLink] = useState("");
  const [busy, setBusy] = useState(false);
  const [settingsVersion, setSettingsVersion] = useState(0);
  const [route, setRoute] = useState(routeOf);

  useEffect(() => {
    const onChange = () => setRoute(routeOf());
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);

  useEffect(() => {
    document.title = "Attesta Wallet";
  }, []);

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
      else if (result.value.kind === "shared") setNotice({ kind: "info", text: `Gedeeld: ${result.value.claims.map(claimLabel).join(", ")} met ${result.value.verifier}.` });
      else setPending(result.value);
    } finally {
      setBusy(false);
    }
  }, []);

  // The QR code from the mail, as an image: the wallet reads it and shows what will be added.
  const openImage = useCallback(async (file: File) => {
    setNotice(null);
    setBusy(true);
    try {
      const result = await window.wallet.prepareImage(new Uint8Array(await file.arrayBuffer()));
      if (!result.ok) setNotice({ kind: "error", text: result.error });
      else if (result.value.kind === "shared") setNotice({ kind: "info", text: `Gedeeld: ${result.value.claims.map(claimLabel).join(", ")} met ${result.value.verifier}.` });
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
      } else if (result.value.kind === "credential") {
        setNotice({ kind: "info", text: `${result.value.credentials.length} credential(s) toegevoegd aan uw wallet.` });
        await refresh();
      } else {
        setNotice({ kind: "info", text: "Uw antwoord is verstuurd." });
      }
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    const result = await window.wallet.remove(id);
    setNotice(result.ok ? { kind: "info", text: "Credential verwijderd." } : { kind: "error", text: result.error });
    await refresh();
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
    <Shell
      title={route.label}
      crumbs={route === HOME ? [] : [{ label: HOME.label, href: HOME.href }]}
      active={route.id}
      footer={credentials.length > 0 ? `${credentials.length} credential(s) in uw wallet` : undefined}
    >
      {notice && (
        <p role="status" className={`notice ${notice.kind}`}>
          {notice.text}
        </p>
      )}

      {route === HOME && <Blocks />}

      {route.id === "credentials" && <CredentialList credentials={credentials} onRemove={(id) => void remove(id)} />}

      {route.id === "qr" && (
        <div
          className="card drop"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            e.preventDefault();
            const file = e.dataTransfer.files[0];
            if (file) void openImage(file);
          }}
        >
          <label>
            QR-code uit de e-mail
            <input
              type="file"
              accept="image/png,image/jpeg"
              aria-label="QR-code"
              disabled={busy}
              onChange={(e) => {
                const file = e.target.files?.[0];
                e.target.value = "";
                if (file) void openImage(file);
              }}
            />
          </label>
          <p>Kies de afbeelding of sleep hem hierheen.</p>
        </div>
      )}

      {route.id === "link" && (
        <form className="card" onSubmit={submit}>
          <label>
            Link
            <input value={link} onChange={(e) => setLink(e.target.value)} placeholder="openid-credential-offer://… of openid4vp://…" aria-label="Link" />
          </label>
          <button type="submit" className="primary" disabled={busy}>
            Openen
          </button>
        </form>
      )}

      {route.id === "delen" && <SharingSettings version={settingsVersion} />}

      {pending && <Consent pending={pending} busy={busy} onApprove={approve} onDecline={decline} />}
    </Shell>
  );
}
