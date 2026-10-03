// Thin typed wrapper around backend/internal/httpapi's HTTP surface. Takes a fetch implementation as a constructor argument (defaulting to the global
// one) so tests can stub it without a real server.
import type {
  AuthorizeRequest,
  AuthorizeResponse,
  LoginRequest,
  LoginResponse,
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

export class ApiClient {
  constructor(
    private readonly baseUrl: string,
    private readonly fetchImpl: typeof fetch = fetch,
  ) {}

  async login(req: LoginRequest): Promise<LoginResponse> {
    return this.postJSON<LoginResponse>("/v1/login", req);
  }

  // authorize submits a proof for verification and policy evaluation. token, if given, is the mock-login JWT — attached for audit/logging only,
  // per backend/internal/httpapi/authorize.go; it never affects the decision.
  async authorize(req: AuthorizeRequest, token?: string): Promise<AuthorizeResponse> {
    return this.postJSON<AuthorizeResponse>("/v1/authorize", req, token ? { Authorization: `Bearer ${token}` } : {});
  }

  // deployPolicy hot-deploys a .gno policy via the admin API. Requires the server's configured admin token.
  async deployPolicy(adminToken: string, req: PutPolicyRequest): Promise<PutPolicyResponse> {
    return this.postJSON<PutPolicyResponse>("/admin/policies", req, { "X-Admin-Token": adminToken });
  }

  private async postJSON<Res>(path: string, body: unknown, headers: Record<string, string> = {}): Promise<Res> {
    const res = await this.fetchImpl(`${this.baseUrl}${path}`, {
      method: "POST",
      headers: { "Content-Type": "application/json", ...headers },
      body: JSON.stringify(body),
    });
    const data = await res.json();
    if (!res.ok) {
      const message = typeof data === "object" && data && "error" in data ? String(data.error) : res.statusText;
      throw new ApiError(res.status, message);
    }
    return data as Res;
  }
}
