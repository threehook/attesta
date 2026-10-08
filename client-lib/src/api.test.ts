import { describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./api.js";

function fakeFetch(status: number, body: unknown): typeof fetch {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: "status",
    json: async () => body,
  }) as unknown as typeof fetch;
}

function sequenceFetch(...bodies: unknown[]): typeof fetch {
  const fn = vi.fn();
  for (const body of bodies) {
    fn.mockResolvedValueOnce({ ok: true, status: 200, statusText: "OK", json: async () => body });
  }
  return fn as unknown as typeof fetch;
}

describe("ApiClient.createAuthorizationRequest", () => {
  it("posts the request and returns the link and id", async () => {
    const fetchImpl = fakeFetch(200, { requestId: "r1", authorizationRequest: "openid4vp://?x=1" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);
    const req = { resource: "vault", policyId: "p1", credentialType: "Diploma", claims: ["degree"] };

    const result = await client.createAuthorizationRequest(req);

    expect(result).toEqual({ requestId: "r1", authorizationRequest: "openid4vp://?x=1" });
    expect(fetchImpl).toHaveBeenCalledWith(
      "http://localhost:8080/v1/authorize/requests",
      expect.objectContaining({ method: "POST", body: JSON.stringify(req) }),
    );
  });

  it("sends the roles in the Att-User-Roles header", async () => {
    const fetchImpl = fakeFetch(200, { requestId: "r1", authorizationRequest: "openid4vp://?x=1" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    await client.createAuthorizationRequest({ resource: "v", policyId: "p1", credentialType: "Diploma" }, { userRoles: ["admin", "editor"] });

    expect(fetchImpl).toHaveBeenCalledWith(
      "http://localhost:8080/v1/authorize/requests",
      expect.objectContaining({ headers: expect.objectContaining({ "Att-User-Roles": "admin,editor" }) }),
    );
  });

  it("throws an ApiError carrying the backend's message", async () => {
    const client = new ApiClient("http://localhost:8080", fakeFetch(404, { error: 'unknown policy "p1"' }));

    await expect(client.createAuthorizationRequest({ resource: "v", policyId: "p1", credentialType: "Diploma" })).rejects.toMatchObject({
      name: "ApiError",
      status: 404,
      message: 'unknown policy "p1"',
    });
    await expect(client.createAuthorizationRequest({ resource: "v", policyId: "p1", credentialType: "Diploma" })).rejects.toBeInstanceOf(ApiError);
  });
});

describe("ApiClient.getAuthorizationOutcome", () => {
  it("gets /v1/authorize/requests/{id} without a body", async () => {
    const fetchImpl = fakeFetch(200, { status: "pending" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    expect(await client.getAuthorizationOutcome("a/b")).toEqual({ status: "pending" });
    expect(fetchImpl).toHaveBeenCalledWith(
      "http://localhost:8080/v1/authorize/requests/a%2Fb",
      expect.objectContaining({ method: "GET", body: undefined }),
    );
  });
});

describe("ApiClient.waitForOutcome", () => {
  it("polls until the decision is done", async () => {
    const allowed = { status: "done", allow: true, reason: "ok", subject: { issuer: "did:key:i", email: "ada@example.com" } };
    const fetchImpl = sequenceFetch({ status: "pending" }, { status: "pending" }, allowed);
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    const outcome = await client.waitForOutcome("r1", { intervalMs: 0 });

    expect(outcome).toEqual(allowed);
    expect(fetchImpl).toHaveBeenCalledTimes(3);
  });

  it("gives up after the timeout", async () => {
    const client = new ApiClient("http://localhost:8080", fakeFetch(200, { status: "pending" }));

    await expect(client.waitForOutcome("r1", { intervalMs: 5, timeoutMs: 10 })).rejects.toThrow("timed out");
  });

  it("stops when aborted", async () => {
    const client = new ApiClient("http://localhost:8080", fakeFetch(200, { status: "pending" }));
    const controller = new AbortController();
    controller.abort();

    await expect(client.waitForOutcome("r1", { signal: controller.signal })).rejects.toThrow();
  });
});

describe("ApiClient.deployPolicy", () => {
  it("sends the admin token header", async () => {
    const fetchImpl = fakeFetch(200, { status: "deployed", id: "p1" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    const result = await client.deployPolicy("admin-tok", { id: "p1", source: "package policy" });

    expect(result).toEqual({ status: "deployed", id: "p1" });
    expect(fetchImpl).toHaveBeenCalledWith(
      "http://localhost:8080/admin/policies",
      expect.objectContaining({ headers: expect.objectContaining({ "X-Admin-Token": "admin-tok" }) }),
    );
  });
});
