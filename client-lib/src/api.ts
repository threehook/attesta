// Thin typed wrapper around backend/internal/httpapi's HTTP surface. Takes a fetch implementation as a constructor argument (defaulting to the global
// one) so tests can stub it without a real server.
import type {
  AuthorizationOutcome,
  AuthorizationRequest,
  AuthorizationRequestResponse,
  PutPolicyRequest,
  PutPolicyResponse,
} from "./types.js";

export class ApiError extends Error {
  constructor(
    public readonly status: number,
    message: string,
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export interface WaitOptions {
  // Time between polls. Defaults to one second.
  intervalMs?: number;
  // How long to wait before giving up. Defaults to the backend's five-minute request lifetime.
  timeoutMs?: number;
  signal?: AbortSignal;
}

export class ApiClient {
  constructor(
    private readonly baseUrl: string,
    // Wrapped, not passed directly: a bare `fetch` reference loses its required `this` binding to `window` once
    // detached from the global object, and browsers reject the call with "Illegal invocation" at call time.
    private readonly fetchImpl: typeof fetch = (...args) => fetch(...args),
  ) {}

  // createAuthorizationRequest starts an authorization. Hand the returned link to the user's wallet, then wait for the decision.
  // userRoles are the user's roles as the calling application knows them; Attesta cannot verify them, so call this from a server, not a browser.
  // traceparent is the W3C trace context of the caller's own span; the decision log keeps its trace.
  async createAuthorizationRequest(
    req: AuthorizationRequest,
    options: { userRoles?: string[]; traceparent?: string } = {},
  ): Promise<AuthorizationRequestResponse> {
    const headers: Record<string, string> = options.userRoles?.length ? { "Att-User-Roles": options.userRoles.join(",") } : {};
    if (options.traceparent) {
      headers["traceparent"] = options.traceparent;
    }
    return this.requestJSON<AuthorizationRequestResponse>("POST", "/v1/authorize/requests", req, headers);
  }

  async getAuthorizationOutcome(requestId: string): Promise<AuthorizationOutcome> {
    return this.requestJSON<AuthorizationOutcome>("GET", `/v1/authorize/requests/${encodeURIComponent(requestId)}`);
  }

  // waitForOutcome polls until the wallet has answered and the backend has decided.
  async waitForOutcome(requestId: string, options: WaitOptions = {}): Promise<Extract<AuthorizationOutcome, { status: "done" }>> {
    const { intervalMs = 1000, timeoutMs = 5 * 60 * 1000, signal } = options;
    const deadline = Date.now() + timeoutMs;
    for (;;) {
      signal?.throwIfAborted();
      const outcome = await this.getAuthorizationOutcome(requestId);
      if (outcome.status === "done") {
        return outcome;
      }
      if (Date.now() + intervalMs > deadline) {
        throw new Error("timed out waiting for the wallet to answer");
      }
      await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
  }

  // deployPolicy hot-deploys a .gno policy via the admin API. Requires the server's configured admin token.
  async deployPolicy(adminToken: string, req: PutPolicyRequest): Promise<PutPolicyResponse> {
    return this.requestJSON<PutPolicyResponse>("POST", "/admin/policies", req, { "X-Admin-Token": adminToken });
  }

  private async requestJSON<Res>(method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<Res> {
    const res = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method,
      headers: { ...(body === undefined ? {} : { "Content-Type": "application/json" }), ...headers },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await res.json();
    if (!res.ok) {
      const message = typeof data === "object" && data && "error" in data ? String(data.error) : res.statusText;
      throw new ApiError(res.status, message);
    }
    return data as Res;
  }
}
