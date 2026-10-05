import { useEffect, useState } from 'react'
import { status as fetchStatus, submit, type Status, type Submitted } from './api.ts'

const POLL_MS = 1000

export default function App() {
  const [postcode, setPostcode] = useState('1111BB')
  const [huisnummer, setHuisnummer] = useState('2')
  const [busy, setBusy] = useState(false)
  const [submitted, setSubmitted] = useState<Submitted | null>(null)
  const [state, setState] = useState<Status | null>(null)
  const [error, setError] = useState<string | null>(null)

  async function dienIn() {
    setError(null)
    setState(null)
    setSubmitted(null)
    setBusy(true)
    try {
      setSubmitted(await submit(postcode, huisnummer))
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(false)
    }
  }

  // Once the wallet link is shown, ask the backend for the outcome until the employee has answered in the wallet.
  useEffect(() => {
    if (!submitted) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const next = await fetchStatus(submitted.requestId)
        if (stopped) return
        setState(next)
        if (next.status === 'pending') timer = setTimeout(poll, POLL_MS)
      } catch (err) {
        if (!stopped) setError(err instanceof Error ? err.message : String(err))
      }
    }
    void poll()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [submitted])

  const waiting = submitted !== null && (state === null || state.status === 'pending')

  return (
    <main>
      <header>
        <h1>Laadpaal aanvraag</h1>
        <p>
          Medewerkers van de gemeente dienen hier een aanvraag in voor een laadpaal. Wie dat mag, bewijst de medewerker met een
          medewerkers-ID uit de eigen wallet; <code>attesta</code> naast de laadpalen-API beslist.
        </p>
      </header>

      {error && <div className="error show">{error}</div>}

      <div className="card">
        <h2>Voor welk adres?</h2>
        <div className="row">
          <div className="field">
            <label htmlFor="postcode">Postcode</label>
            <input type="text" id="postcode" value={postcode} onChange={(e) => setPostcode(e.target.value)} />
          </div>
          <div className="field narrow">
            <label htmlFor="huisnummer">Huisnummer</label>
            <input type="text" id="huisnummer" value={huisnummer} onChange={(e) => setHuisnummer(e.target.value)} />
          </div>
        </div>
        <p className="muted">Bekende adressen: 1111BB 2 (vrij), 1111AA 1 (laadpaal aanwezig), 1111DD 4 (geen elektrisch voertuig).</p>
      </div>

      <button className="primary" onClick={dienIn} disabled={busy || waiting}>
        {busy ? 'Bezig...' : waiting ? 'Wacht op de wallet...' : 'Dien aanvraag in'}
      </button>

      {submitted && waiting && (
        <div className="card wallet-link" style={{ marginTop: 16 }}>
          <h2>Bevestig in je wallet</h2>
          <p className="muted">Open de wallet en bevestig het delen van je gegevens.</p>
          <a className="open-wallet" href={submitted.authorizationRequest}>
            Open in wallet
          </a>
          <p className="muted">Opent de wallet niet? Plak dan deze link erin:</p>
          <code>{submitted.authorizationRequest}</code>
        </div>
      )}

      {state?.status === 'expired' && (
        <div className="result show warn">
          <span className="badge">⏱</span>
          <span className="text">
            <strong>Verlopen</strong>
            <span>{state.reason}</span>
          </span>
        </div>
      )}

      {state?.status === 'done' && !state.authorized && (
        <div className="result show deny">
          <span className="badge">⛔</span>
          <span className="text">
            <strong>Niet bevoegd om een aanvraag in te dienen</strong>
            <span>{state.reason}</span>
          </span>
        </div>
      )}

      {state?.status === 'done' && state.authorized && state.result && (
        <div className={`result show ${state.result.granted ? 'allow' : 'deny'}`}>
          <span className="badge">{state.result.granted ? '✅' : '❌'}</span>
          <span className="text">
            <strong>{state.result.granted ? 'Toegekend' : 'Niet toegekend'}</strong>
            <span>{state.result.reason}</span>
            {state.subject && (
              <span className="who">
                Ingediend door {state.subject.email} (afgegeven door {state.subject.issuer})
              </span>
            )}
          </span>
        </div>
      )}

      {submitted && (
        <details>
          <summary>Toon API-aanroep</summary>
          <pre>{JSON.stringify({ requestId: submitted.requestId, authorizationRequest: submitted.authorizationRequest }, null, 2)}</pre>
          <pre>{state?.debug.outcome ? JSON.stringify(state.debug.outcome, null, 2) : 'Nog geen antwoord van attesta.'}</pre>
        </details>
      )}
    </main>
  )
}
