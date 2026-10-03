import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import * as snarkjs from "snarkjs";
import { describe, expect, it } from "vitest";
import { buildProof } from "./proof.js";

const CIRCUIT_DIR = fileURLToPath(new URL("../circuits/cubic/build/", import.meta.url));
const WASM_PATH = `${CIRCUIT_DIR}cubic_js/cubic.wasm`;
const ZKEY_PATH = `${CIRCUIT_DIR}cubic_final.zkey`;
const VKEY = JSON.parse(readFileSync(`${CIRCUIT_DIR}verification_key.json`, "utf8"));

describe("buildProof", () => {
  it("builds a proof snarkjs itself accepts as valid, for x=3 (3^3+3+5=35)", async () => {
    const { proof, publicSignals } = await buildProof({ x: 3 }, { wasmPath: WASM_PATH, zkeyPath: ZKEY_PATH });

    expect(publicSignals).toEqual(["35"]);
    await expect(snarkjs.groth16.verify(VKEY, publicSignals, proof)).resolves.toBe(true);
  });

  it("produces a proof the backend's verifier would reject if the public signal is tampered with", async () => {
    const { proof } = await buildProof({ x: 3 }, { wasmPath: WASM_PATH, zkeyPath: ZKEY_PATH });

    await expect(snarkjs.groth16.verify(VKEY, ["999"], proof)).resolves.toBe(false);
  });
});
