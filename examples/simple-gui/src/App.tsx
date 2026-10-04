import { useState } from "react";
import type { Credential } from "@zk-puoi/client";
import { LoginView } from "./components/LoginView.js";
import { WalletView } from "./components/WalletView.js";
import { ResourceView } from "./components/ResourceView.js";
import { wallet } from "./lib/clients.js";

type Tab = "login" | "wallet" | "resource";

function App() {
  const [tab, setTab] = useState<Tab>("login");
  const [subject, setSubject] = useState<string | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [credentials, setCredentials] = useState<Credential[]>(() => wallet.list());

  function handleLogin(newSubject: string, newToken: string) {
    setSubject(newSubject);
    setToken(newToken);
    setTab("wallet");
  }

  function handleLogout() {
    setSubject(null);
    setToken(null);
    setTab("login");
  }

  return (
    <>
      <h1>zk-puoi example GUI</h1>
      <nav>
        <button type="button" aria-current={tab === "login" ? "page" : undefined} onClick={() => setTab("login")}>
          Login
        </button>
        <button type="button" aria-current={tab === "wallet" ? "page" : undefined} onClick={() => setTab("wallet")}>
          Wallet
        </button>
        <button
          type="button"
          aria-current={tab === "resource" ? "page" : undefined}
          disabled={!token}
          onClick={() => setTab("resource")}
        >
          Request resource
        </button>
      </nav>

      {tab === "login" && <LoginView subject={subject} onLogin={handleLogin} onLogout={handleLogout} />}
      {tab === "wallet" && <WalletView credentials={credentials} onChange={() => setCredentials(wallet.list())} />}
      {tab === "resource" && token && <ResourceView token={token} credentials={credentials} />}
    </>
  );
}

export default App;
