import type { WalletAgent } from "@attesta/wallet";
import { describe, expect, it } from "vitest";
import { WalletError, WalletService } from "./service.js";

// These cases never reach the agent: they cover what the service decides on its own. Reading real offers and requests is covered end to end
// (e2e/wallet.spec.ts).
const service = () => new WalletService({} as WalletAgent);

describe("WalletService", () => {
  it("rejects a link that is neither an offer nor a presentation request", async () => {
    await expect(service().prepare("https://example.com")).rejects.toThrow(WalletError);
    await expect(service().prepare("")).rejects.toThrow("not a credential offer");
  });

  it("rejects approving something it never prepared", async () => {
    await expect(service().approve("unknown")).rejects.toThrow("expired");
  });

  it("forgets what was declined", async () => {
    const s = service();
    s.decline("anything");
    await expect(s.approve("anything")).rejects.toThrow(WalletError);
  });
});
