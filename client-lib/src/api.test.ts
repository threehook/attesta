import { describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError } from "./api.js";
import type { Groth16Proof } from "./types.js";

const PROOF: Groth16Proof = {
  pi_a: ["1", "2", "1"],
  pi_b: [
    ["1", "2"],
    ["3", "4"],
    ["1", "0"],
  ],
  pi_c: ["5", "6", "1"],
  protocol: "groth16",
  curve: "bn128",
};

function fakeFetch(status: number, body: unknown): typeof fetch {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    statusText: "status",
    json: async () => body,
  }) as unknown as typeof fetch;
}

describe("ApiClient.login", () => {
  it("posts to /v1/login and returns the token", async () => {
    const fetchImpl = fakeFetch(200, { token: "abc" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    const result = await client.login({ subject: "alice", roles: ["student"] });

    expect(result).toEqual({ token: "abc" });
    expect(fetchImpl).toHaveBeenCalledWith(
      "http://localhost:8080/v1/login",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ subject: "alice", roles: ["student"] }),
      }),
    );
  });
});

describe("ApiClient.authorize", () => {
  it("attaches a bearer token when given one", async () => {
    const fetchImpl = fakeFetch(200, { allow: true, reason: "ok" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    const result = await client.authorize({ resource: "vault", policyId: "p1", proof: PROOF, publicSignals: ["35"] }, "tok");

    expect(result).toEqual({ allow: true, reason: "ok" });
    const [, init] = (fetchImpl as ReturnType<typeof vi.fn>).mock.calls[0];
    expect((init.headers as Record<string, string>).Authorization).toBe("Bearer tok");
  });

  it("omits the Authorization header when no token is given", async () => {
    const fetchImpl = fakeFetch(200, { allow: false, reason: "no" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    await client.authorize({ resource: "vault", policyId: "p1", proof: PROOF, publicSignals: ["35"] });

    const [, init] = (fetchImpl as ReturnType<typeof vi.fn>).mock.calls[0];
    expect((init.headers as Record<string, string>).Authorization).toBeUndefined();
  });

  it("throws ApiError with the server's message on a non-2xx response", async () => {
    const fetchImpl = fakeFetch(404, { error: "unknown policy \"x\"" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    await expect(client.authorize({ resource: "r", policyId: "p1", proof: PROOF, publicSignals: [] })).rejects.toMatchObject({
      status: 404,
      message: 'unknown policy "x"',
    });
  });
});

describe("ApiClient.deployPolicy", () => {
  it("sends the admin token header", async () => {
    const fetchImpl = fakeFetch(200, { status: "deployed", id: "p1" });
    const client = new ApiClient("http://localhost:8080", fetchImpl);

    const result = await client.deployPolicy("admin-tok", { id: "p1", source: "package policy" });

    expect(result).toEqual({ status: "deployed", id: "p1" });
    const [, init] = (fetchImpl as ReturnType<typeof vi.fn>).mock.calls[0];
    expect((init.headers as Record<string, string>)["X-Admin-Token"]).toBe("admin-tok");
  });
});

it("ApiError carries the HTTP status", () => {
  const err = new ApiError(401, "nope");
  expect(err.status).toBe(401);
  expect(err.message).toBe("nope");
});
