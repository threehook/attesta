import nodemailer from 'nodemailer'

export interface Mail {
  to: string
  subject: string
  text: string
  html?: string
  attachments?: Array<{ filename: string; content: Buffer; contentType: string; /** Lets the html show the file inline instead of offering it as a file. */ cid?: string }>
}

/** Where the issuer's mail goes out. An organisation's own mail server is one implementation, a mail catcher another. */
export interface Mailer {
  send(mail: Mail): Promise<void>
}

/** Sends through an SMTP server; in the demo that is a mail catcher that keeps the mails in the cluster. */
export class SmtpMailer implements Mailer {
  private readonly transport

  constructor(
    options: { host: string; port: number },
    private readonly from: string,
  ) {
    this.transport = nodemailer.createTransport({ host: options.host, port: options.port, secure: false, ignoreTLS: true })
  }

  async send(mail: Mail) {
    await this.transport.sendMail({ from: this.from, ...mail })
  }
}
