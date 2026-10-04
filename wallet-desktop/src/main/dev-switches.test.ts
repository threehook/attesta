import { describe, expect, it } from "vitest";
import { devSwitches } from "./dev-switches.js";

const env = { ATTESTA_WALLET_DATA_DIR: "/tmp/wallet", ATTESTA_WALLET_KEY: "a-key", ATTESTA_ALLOW_INSECURE_HTTP: "1" };

describe("devSwitches", () => {
  it("honours the environment when running from source", () => {
    expect(devSwitches(env, false)).toEqual({ dataDir: "/tmp/wallet", storeKey: "a-key", allowInsecureHttp: true });
  });

  it("ignores every switch in an installed app", () => {
    expect(devSwitches(env, true)).toEqual({ allowInsecureHttp: false });
  });

  it("defaults to the safe settings when nothing is set", () => {
    expect(devSwitches({}, false)).toEqual({ dataDir: undefined, storeKey: undefined, allowInsecureHttp: false });
  });

  it("treats empty values as unset and only 1 as enabling plain http", () => {
    expect(devSwitches({ ATTESTA_WALLET_KEY: "", ATTESTA_WALLET_DATA_DIR: "", ATTESTA_ALLOW_INSECURE_HTTP: "true" }, false)).toEqual({
      dataDir: undefined,
      storeKey: undefined,
      allowInsecureHttp: false,
    });
  });
});
