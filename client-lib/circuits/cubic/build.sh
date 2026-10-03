#!/usr/bin/env bash
# Compiles cubic.circom and runs a fresh (non-ceremony, toy) Groth16 trusted setup, producing the artifacts client-lib and the backend actually need
# at runtime: cubic_js/cubic.wasm (the witness calculator, used to build a proof in the browser), cubic_final.zkey (the proving key), and
# verification_key.json (the verifying key).
#
# The trusted setup step uses randomness, so re-running this script produces different (but equally valid) keys each time — it does not reproduce the
# committed build/ output byte for byte. Don't re-run this casually: anyone with an old proving key can no longer produce proofs the new
# verification_key.json accepts, and vice versa. Only re-run it when the circuit itself changes, then commit the new build/ output as one unit.
set -euo pipefail
cd "$(dirname "$0")"

SNARKJS="$(pwd)/../../node_modules/.bin/snarkjs"
PTAU_POWER=12 # supports up to 2^12 constraints; this circuit uses 3

rm -rf build
mkdir build
circom cubic.circom --r1cs --wasm --sym -o build

cd build
"$SNARKJS" powersoftau new bn128 "$PTAU_POWER" pot_0000.ptau -v
"$SNARKJS" powersoftau contribute pot_0000.ptau pot_0001.ptau --name="zk-puoi toy setup" -v -e="$(head -c 64 /dev/urandom | base64)"
"$SNARKJS" powersoftau prepare phase2 pot_0001.ptau pot_final.ptau -v

"$SNARKJS" groth16 setup cubic.r1cs pot_final.ptau cubic_0000.zkey
"$SNARKJS" zkey contribute cubic_0000.zkey cubic_final.zkey --name="zk-puoi toy setup" -v -e="$(head -c 64 /dev/urandom | base64)"
"$SNARKJS" zkey export verificationkey cubic_final.zkey verification_key.json

rm -f pot_0000.ptau pot_0001.ptau pot_final.ptau cubic_0000.zkey

echo "Built build/cubic_js/cubic.wasm, build/cubic_final.zkey, build/verification_key.json"
