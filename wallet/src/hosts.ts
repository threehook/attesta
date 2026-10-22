/**
 * Reads `https://public.example=http://internal:4001,...` into a map from a public address to the address inside the cluster, where the public name does
 * not resolve. The backend has the same setting for status lists (ATTESTA_STATUS_HOSTS).
 */
export function parseHosts(text: string): Record<string, string> {
  const hosts: Record<string, string> = {}
  for (const pair of text.split(',')) {
    const entry = pair.trim()
    if (entry === '') continue
    const at = entry.indexOf('=')
    const from = entry.slice(0, at).replace(/\/+$/, '')
    const to = entry.slice(at + 1).replace(/\/+$/, '')
    if (at < 1 || !/^https?:\/\/[^/]+$/.test(from) || !/^https?:\/\/[^/]+$/.test(to)) {
      throw new Error(`"${entry}" is not public-address=internal-address, such as https://issuer.example=http://issuer:4001`)
    }
    hosts[from] = to
  }
  return hosts
}

/** A fetch that sends what is meant for a public address to its internal address, and everything else as it is. */
export function mappedFetch(base: typeof fetch, hosts: Record<string, string>): typeof fetch {
  const map = (url: string): string | undefined => {
    for (const [from, to] of Object.entries(hosts)) {
      if (url === from || url.startsWith(`${from}/`) || url.startsWith(`${from}?`)) return to + url.slice(from.length)
    }
    return undefined
  }
  return (input, init) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url
    const mapped = map(url)
    if (mapped === undefined) return base(input, init)
    return base(typeof input === 'string' || input instanceof URL ? mapped : new Request(mapped, input), init)
  }
}
