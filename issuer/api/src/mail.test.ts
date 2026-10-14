import type { AddressInfo } from 'node:net'
import { SMTPServer, type SMTPServerOptions } from 'smtp-server'
import { afterEach, describe, expect, it } from 'vitest'
import { SmtpMailer } from './mail.js'

const mail = {
  to: 'jerry@example.com',
  subject: 'Uw credential staat klaar',
  text: 'tekst',
  html: '<p>html</p>',
  attachments: [{ filename: 'credential.png', content: Buffer.from('png'), contentType: 'image/png' }],
}

interface Received {
  secure: boolean
  user?: string
  to: string[]
  data: string
}

// A mail server of the test's own, to see what the mail sender does: whether it logs in, whether it secures the connection, and what it sends.
describe('SmtpMailer', () => {
  let server: SMTPServer | undefined
  const received: Received[] = []

  afterEach(async () => {
    await new Promise((resolve) => server?.close(() => resolve(undefined)))
    server = undefined
    received.length = 0
  })

  async function start(options: SMTPServerOptions = {}): Promise<number> {
    server = new SMTPServer({
      authOptional: true,
      disabledCommands: [],
      onAuth(auth, _session, done) {
        if (auth.username === 'api-user' && auth.password === 'secret-key') done(null, { user: auth.username })
        else done(new Error('Invalid login'))
      },
      onData(stream, session, done) {
        let data = ''
        stream.on('data', (chunk: Buffer) => (data += chunk.toString()))
        stream.on('end', () => {
          received.push({ secure: session.secure, user: typeof session.user === 'string' ? session.user : undefined, to: session.envelope.rcptTo.map((r) => r.address), data })
          done()
        })
      },
      ...options,
    })
    await new Promise<void>((resolve) => server!.listen(0, resolve))
    return (server.server.address() as AddressInfo).port
  }

  it('sends to a mail catcher that asks for nothing, without logging in or securing the connection', async () => {
    const port = await start()

    await new SmtpMailer({ host: 'localhost', port }, 'credentials@attesta.example').send(mail)

    expect(received).toHaveLength(1)
    expect(received[0]).toMatchObject({ secure: false, user: undefined, to: ['jerry@example.com'] })
    expect(received[0].data).toContain('From: credentials@attesta.example')
    expect(received[0].data).toContain('Subject: Uw credential staat klaar')
    expect(received[0].data).toContain('filename=credential.png')
  })

  it('logs in and secures the connection with STARTTLS when it has a user', async () => {
    const port = await start({ authOptional: false })

    await new SmtpMailer({ host: 'localhost', port, user: 'api-user', password: 'secret-key', allowSelfSigned: true }, 'credentials@attesta.example').send(mail)

    expect(received).toHaveLength(1)
    expect(received[0]).toMatchObject({ secure: true, user: 'api-user', to: ['jerry@example.com'] })
  })

  it('does not send when the login is wrong', async () => {
    const port = await start({ authOptional: false })

    await expect(new SmtpMailer({ host: 'localhost', port, user: 'api-user', password: 'wrong', allowSelfSigned: true }, 'a@b.example').send(mail)).rejects.toThrow(/Invalid login/)

    expect(received).toEqual([])
  })

  it('does not hand over its password when the server cannot secure the connection', async () => {
    // The server would take a login in the clear, so only the mail sender's own refusal keeps the password back.
    const port = await start({ authOptional: false, disabledCommands: ['STARTTLS'], allowInsecureAuth: true })

    await expect(new SmtpMailer({ host: 'localhost', port, user: 'api-user', password: 'secret-key', allowSelfSigned: true }, 'a@b.example').send(mail)).rejects.toThrow()

    expect(received).toEqual([])
  })

  it('does not trust a server whose certificate it cannot verify, unless told to', async () => {
    const port = await start({ authOptional: false })

    await expect(new SmtpMailer({ host: 'localhost', port, user: 'api-user', password: 'secret-key' }, 'a@b.example').send(mail)).rejects.toThrow()

    expect(received).toEqual([])
  })
})
