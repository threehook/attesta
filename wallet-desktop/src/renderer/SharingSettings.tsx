import { useCallback, useEffect, useState } from "react";
import type { Result, Settings } from "../shared/api.js";

// version changes whenever something outside this section may have changed the settings, such as a choice to remember in a confirmation.
export function SharingSettings({ version }: { version: number }) {
  const [settings, setSettings] = useState<Settings | null>(null);
  const [error, setError] = useState<string | null>(null);

  const apply = useCallback((result: Result<Settings>) => {
    if (result.ok) {
      setSettings(result.value);
      setError(null);
    } else {
      setError(result.error);
    }
  }, []);

  useEffect(() => {
    void window.wallet.settings().then(apply);
  }, [apply, version]);

  if (!settings) return error ? <p className="error">{error}</p> : null;

  return (
    <section aria-label="Delen">
      <h3>Applicaties waarbij ik automatisch aanmeld</h3>
      {settings.signIns.length === 0 ? (
        <p>Geen.</p>
      ) : (
        <ul>
          {settings.signIns.map((choice) => (
            <li key={`${choice.application} ${choice.email} ${choice.issuer}`}>
              {choice.application} als {choice.email} ({choice.issuer}){" "}
              <button type="button" onClick={() => void window.wallet.forgetSignIn(choice).then(apply)}>
                Verwijderen
              </button>
            </li>
          ))}
        </ul>
      )}
      <h3>Applicaties waarmee ik altijd deel</h3>
      {settings.trustedApplications.length === 0 ? (
        <p>Geen.</p>
      ) : (
        <ul>
          {settings.trustedApplications.map((application) => (
            <li key={application}>
              {application}{" "}
              <button type="button" onClick={() => void window.wallet.forgetApplication(application).then(apply)}>
                Verwijderen
              </button>
            </li>
          ))}
        </ul>
      )}
      {error && <p className="error">{error}</p>}
    </section>
  );
}
