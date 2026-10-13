import { startGui } from './gui.js'

const port = Number(process.env.ATTESTA_ISSUER_GUI_LISTEN_PORT ?? 4000)
if (!Number.isInteger(port) || port < 0 || port > 65535) {
  throw new Error(`ATTESTA_ISSUER_GUI_LISTEN_PORT must be a port number, got "${process.env.ATTESTA_ISSUER_GUI_LISTEN_PORT}"`)
}
const apiUrl = process.env.ATTESTA_ISSUER_API_URL ?? 'http://localhost:4001'

await startGui({ port, apiUrl })
console.log(`issuer pages listening on ${port}, issuer API at ${apiUrl}`)
