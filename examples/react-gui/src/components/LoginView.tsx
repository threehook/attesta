import { useState } from "react";
import { apiClient } from "../lib/clients.js";

interface Props {
  subject: string | null;
  onLogin: (subject: string, token: string) => void;
  onLogout: () => void;
}

// LoginView is the mock login: no credential check happens here at all, it just asks the backend for a JWT carrying whatever subject/roles were typed
// in. The JWT identifies the session for audit/logging on the backend (see backend/internal/httpapi/login.go) — it plays no part in the /v1/authorize
// allow/deny decision.
export function LoginView({ subject, onLogin, onLogout }: Props) {
  const [subjectInput, setSubjectInput] = useState("alice");
  const [roles, setRoles] = useState("student");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  if (subject) {
    return (
      <section>
        <p>
          Logged in as <strong>{subject}</strong> (mock login — no credential was actually checked).
        </p>
        <button type="button" onClick={onLogout}>
          Log out
        </button>
      </section>
    );
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setPending(true);
    try {
      const roleList = roles
        .split(",")
        .map((r) => r.trim())
        .filter(Boolean);
      const { token } = await apiClient.login({ subject: subjectInput, roles: roleList });
      onLogin(subjectInput, token);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <section>
      <form onSubmit={handleSubmit}>
        <fieldset>
          <label>
            Subject
            <input value={subjectInput} onChange={(e) => setSubjectInput(e.target.value)} required />
          </label>
          <label>
            Roles (comma-separated)
            <input value={roles} onChange={(e) => setRoles(e.target.value)} />
          </label>
        </fieldset>
        <button type="submit" disabled={pending}>
          {pending ? "Logging in…" : "Log in (mock)"}
        </button>
      </form>
      {error && <p className="error">{error}</p>}
    </section>
  );
}
