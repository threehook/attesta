// Wraps snarkjs's groth16.fullProve for building a proof against a circuit's compiled witness calculator (.wasm) and proving key (.zkey). Works in
// both the browser (pass URLs, which snarkjs fetches) and Node (pass file paths, used by this package's own tests and by
// client-lib/circuits/*/prove.sh's CLI equivalent).
import * as snarkjs from "snarkjs";
import type { Groth16Proof, PublicSignals } from "./types.js";

export interface ZkArtifacts {
  wasmPath: string | Uint8Array;
  zkeyPath: string | Uint8Array;
}

export interface ProveResult {
  proof: Groth16Proof;
  publicSignals: PublicSignals;
}

// buildProof generates a Groth16 proof for input against the circuit compiled into artifacts, returning exactly the shapes backend/internal/httpapi
// expects for /v1/authorize's "proof" and "publicSignals" fields.
export async function buildProof(
  input: Record<string, snarkjs.SignalValueType>,
  artifacts: ZkArtifacts,
): Promise<ProveResult> {
  const { proof, publicSignals } = await snarkjs.groth16.fullProve(input, artifacts.wasmPath, artifacts.zkeyPath);
  return { proof: toGroth16Proof(proof), publicSignals };
}

// toGroth16Proof narrows snarkjs's loosely-typed Groth16Proof (string[] / string[][], no fixed length) into this package's precise wire type, failing
// fast if snarkjs ever changes its proof shape.
function toGroth16Proof(raw: snarkjs.Groth16Proof): Groth16Proof {
  if (raw.pi_a.length !== 3 || raw.pi_c.length !== 3) {
    throw new Error(`unexpected pi_a/pi_c length: ${raw.pi_a.length}/${raw.pi_c.length}, want 3`);
  }
  if (raw.pi_b.length !== 3 || raw.pi_b.some((row) => row.length !== 2)) {
    throw new Error("unexpected pi_b shape, want 3x2");
  }
  if (raw.protocol !== "groth16") {
    throw new Error(`unexpected protocol ${raw.protocol}, want groth16`);
  }
  return {
    pi_a: [raw.pi_a[0], raw.pi_a[1], raw.pi_a[2]],
    pi_b: [
      [raw.pi_b[0][0], raw.pi_b[0][1]],
      [raw.pi_b[1][0], raw.pi_b[1][1]],
      [raw.pi_b[2][0], raw.pi_b[2][1]],
    ],
    pi_c: [raw.pi_c[0], raw.pi_c[1], raw.pi_c[2]],
    protocol: "groth16",
    curve: raw.curve,
  };
}
