import { useCallback, useEffect, useRef, useState } from 'react'
import {
  session as fetchSession,
  config as fetchConfig,
  signIn,
  signInStatus,
  signOut,
  status as fetchStatus,
  submit,
  type Session,
  type Status,
  type Submitted,
} from './api.ts'
import { identities as fetchIdentities, shortDid, withIdentity, type Identity } from './wallet.ts'

const POLL_MS = 1000
const TOAST_MS = 5000

type Flow = 'signIn' | 'request'

export default function App() {
  const [postcode, setPostcode] = useState('1111BB')
  const [huisnummer, setHuisnummer] = useState('2')
  const [busy, setBusy] = useState(false)
  const [flow, setFlow] = useState<Flow>('signIn')
  const [submitted, setSubmitted] = useState<Submitted | null>(null)
  const [state, setState] = useState<Status | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [session, setSession] = useState<Session | null>(null)
  const [wallet, setWallet] = useState<{ found: Identity[] } | 'searching' | 'missing'>('searching')
  const [chosen, setChosen] = useState(0)
  const triedAutomatic = useRef(false)
  const [toast, setToast] = useState<{ kind: 'ok' | 'error'; text: string } | null>(null)

  const refreshSession = () => fetchSession().then(setSession, () => setSession(null))

  useEffect(() => {
    void refreshSession()
  }, [])

  // Asks the wallet who the user is; the wallet is found again when the user comes back to this page.
  const findIdentities = useCallback(async () => {
    try {
      const { application } = await fetchConfig()
      setWallet({ found: await fetchIdentities(application) })
    } catch {
      setWallet('missing')
    }
  }, [])

  useEffect(() => {
    void findIdentities()
    window.addEventListener('focus', findIdentities)
    return () => window.removeEventListener('focus', findIdentities)
  }, [findIdentities])

  // A signed-out employee whose wallet signs in automatically is signed in without the button, once per visit.
  useEffect(() => {
    if (triedAutomatic.current || session === null || session.active || typeof wallet !== 'object') return
    const automatic = wallet.found.find((identity) => identity.automatic)
    if (!automatic) return
    triedAutomatic.current = true
    void start('signIn', () => signIn(), automatic)
  }, [session, wallet])

  // A successful sign-in goes away by itself; a refusal stays until it is closed.
  useEffect(() => {
    if (toast?.kind !== 'ok') return
    const timer = setTimeout(() => setToast(null), TOAST_MS)
    return () => clearTimeout(timer)
  }, [toast])

  async function afmelden() {
    triedAutomatic.current = true
    await signOut()
    await refreshSession()
  }

  // Hands the request to the wallet; there is nothing for the employee to click.
  async function start(kind: Flow, begin: () => Promise<Submitted>, identity?: Identity) {
    setError(null)
    setToast(null)
    setState(null)
    setSubmitted(null)
    setBusy(true)
    try {
      const begun = await begin()
      const started = identity ? { ...begun, authorizationRequest: withIdentity(begun.authorizationRequest, identity) } : begun
      setFlow(kind)
      setSubmitted(started)
      window.location.assign(started.authorizationRequest)
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err))
      void refreshSession()
    } finally {
      setBusy(false)
    }
  }

  const identity = typeof wallet === 'object' ? (wallet.found[chosen] ?? wallet.found[0]) : undefined
  const meldAan = () => start('signIn', signIn, identity)
  const dienIn = () => start('request', () => submit(postcode, huisnummer))

  // Once the wallet link is shown, ask the backend for the outcome until the employee has answered in the wallet.
  useEffect(() => {
    if (!submitted) return
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    const poll = async () => {
      try {
        const next = await (flow === 'signIn' ? signInStatus : fetchStatus)(submitted.requestId)
        if (stopped) return
        setState(next)
        if (next.status === 'pending') timer = setTimeout(poll, POLL_MS)
        else {
          void refreshSession()
          if (flow === 'signIn' && next.status === 'done') {
            setToast(
              next.authorized && next.subject
                ? { kind: 'ok', text: `Aangemeld als ${next.subject.email}` }
                : { kind: 'error', text: `Aanmelden geweigerd: ${next.reason ?? 'onbekende reden'}` },
            )
          }
        }
      } catch (err) {
        if (!stopped) setError(err instanceof Error ? err.message : String(err))
      }
    }
    void poll()
    return () => {
      stopped = true
      clearTimeout(timer)
    }
  }, [submitted, flow])

  const waiting = submitted !== null && (state === null || state.status === 'pending')

  return (
    <main>
      <header>
        <h1>Laadpaal aanvraag</h1>
        <p>
          Medewerkers van de gemeente dienen hier een aanvraag in voor een laadpaal.
          <br />U logt in door middel van het kiezen van uw medewerkers-ID gekoppeld aan uw emailadres.
        </p>
      </header>

      {error && <div className="error show">{error}</div>}

      {session?.active && session.subject && (
        <p className="muted session">
          Aangemeld als {session.subject.email}
          {session.expiresAt && ` tot ${new Date(session.expiresAt).toLocaleTimeString('nl-NL', { hour: '2-digit', minute: '2-digit' })}`}.{' '}
          <button className="link" onClick={afmelden}>
            Afmelden
          </button>
        </p>
      )}

      {session?.active ? (
        <>
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

          <button className="primary" onClick={dienIn} disabled={busy}>
            {busy ? 'Bezig...' : waiting && flow === 'request' ? 'Opnieuw indienen' : 'Dien aanvraag in'}
          </button>
        </>
      ) : (
        <div className="card">
          <h2>Aanmelden</h2>
          {typeof wallet === 'object' && wallet.found.length > 0 ? (
            <div className="field">
              <label htmlFor="identity">E-mailadres</label>
              <select id="identity" value={Math.min(chosen, wallet.found.length - 1)} onChange={(e) => setChosen(Number(e.target.value))}>
                {wallet.found.map((found, i) => (
                  <option key={`${found.email} ${found.issuer}`} value={i} title={found.issuer}>
                    {found.email} ({shortDid(found.issuer)})
                  </option>
                ))}
              </select>
            </div>
          ) : (
            <p className="muted">
              {wallet === 'searching'
                ? 'Uw wallet wordt gezocht...'
                : wallet === 'missing'
                  ? 'Uw wallet is niet gevonden. Start de wallet en kom terug naar deze pagina.'
                  : 'Uw wallet bevat nog geen medewerker-ID.'}
            </p>
          )}
          <button className="primary" onClick={meldAan} disabled={busy || !identity}>
            {busy ? 'Bezig...' : waiting && flow === 'signIn' ? 'Opnieuw aanmelden' : 'Aanmelden'}
          </button>
        </div>
      )}

      {waiting && submitted && (
        <p className="muted">
          Wacht op de wallet. <a href={submitted.authorizationRequest}>Open in wallet</a>
        </p>
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

      {flow === 'request' && state?.status === 'done' && !state.authorized && (
        <div className="result show deny">
          <span className="badge">⛔</span>
          <span className="text">
            <strong>Niet bevoegd om een aanvraag in te dienen</strong>
            <span>{state.reason}</span>
          </span>
        </div>
      )}

      {flow === 'request' && state?.status === 'done' && state.authorized && state.result && (
        <div className={`result show ${state.result.granted ? 'allow' : 'deny'}`}>
          <span className="badge">{state.result.granted ? '✅' : '❌'}</span>
          <span className="text">
            <strong>{state.result.granted ? 'Toegekend' : 'Niet toegekend'}</strong>
            <span>{state.result.reason}</span>
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

      {toast && (
        <div className={`toast ${toast.kind}`} role={toast.kind === 'error' ? 'alert' : 'status'}>
          <span>{toast.text}</span>
          {toast.kind === 'error' && (
            <button className="link" onClick={() => setToast(null)} aria-label="Sluiten">
              ×
            </button>
          )}
        </div>
      )}
    </main>
  )
}
