import nodemailer from 'nodemailer'

export interface Mail {
  to: string
  subject: string
  text: string
  html?: string
  attachments?: Array<{ filename: string; content: Buffer; contentType: string; /** Lets the html show the file inline instead of attaching it as a file. */ cid?: string }>
}

/** Where the issuer's mail goes out. An organisation's own mail server is one implementation, a mail catcher another. */
export interface Mailer {
  send(mail: Mail): Promise<void>
}

/** How the connection to the mail server is secured: not at all (a mail catcher in the cluster), STARTTLS (port 587) or TLS from the start (port 465). */
export type SmtpTls = 'none' | 'starttls' | 'tls'

export interface SmtpOptions {
  host: string
  port: number
  /** Without a user the mail goes out without logging in. */
  user?: string
  password?: string
  /** Defaults to `none` without a user, `tls` on port 465 and `starttls` otherwise. */
  tls?: SmtpTls
  /** Lets a test use a server with a certificate of its own. */
  allowSelfSigned?: boolean
}

/** Sends through an SMTP server: a mail catcher in the demo, a real mail server when an organisation sets one. */
export class SmtpMailer implements Mailer {
  private readonly transport

  constructor(
    options: SmtpOptions,
    private readonly from: string,
  ) {
    const tls = options.tls ?? (options.user ? (options.port === 465 ? 'tls' : 'starttls') : 'none')
    this.transport = nodemailer.createTransport({
      host: options.host,
      port: options.port,
      secure: tls === 'tls',
      ignoreTLS: tls === 'none',
      requireTLS: tls === 'starttls',
      auth: options.user ? { user: options.user, pass: options.password ?? '' } : undefined,
      tls: options.allowSelfSigned ? { rejectUnauthorized: false } : undefined,
    })
  }

  async send(mail: Mail) {
    await this.transport.sendMail({ from: this.from, ...mail })
  }
}
