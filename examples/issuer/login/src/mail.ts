import nodemailer from 'nodemailer'

export interface Mail {
  to: string
  subject: string
  text: string
}

export interface Mailer {
  send(mail: Mail): Promise<void>
}

/** Sends through an SMTP server; in the demo that is a mail catcher. */
export class SmtpMailer implements Mailer {
  private readonly transport

  constructor(
    options: { host: string; port: number },
    private readonly from: string,
  ) {
    this.transport = nodemailer.createTransport({ host: options.host, port: options.port, secure: false, ignoreTLS: true })
  }

  async send({ to, subject, text }: Mail) {
    await this.transport.sendMail({ from: this.from, to, subject, text })
  }
}
