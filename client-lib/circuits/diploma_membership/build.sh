#!/usr/bin/env bash
# Compiles diploma_membership.circom and runs a fresh (non-ceremony, toy) Groth16 trusted setup, producing the
# artifacts client-lib and the backend actually need at runtime: diploma_membership_js/diploma_membership.wasm
# (the witness calculator), diploma_membership_final.zkey (the proving key), and verification_key.json (the
# verifying key). Run registry.mjs separately afterwards (or first — order doesn't matter) to (re)generate the
# demo credential registry this circuit's proofs are checked against.
#
# The trusted setup step uses randomness, so re-running this script produces different (but equally valid) keys
# each time — it does not reproduce the committed build/ output byte for byte. Don't re-run this casually:
# anyone with an old proving key can no longer produce proofs the new verification_key.json accepts, and vice
# versa. Only re-run it when the circuit itself changes, then commit the new build/ output as one unit.
set -euo pipefail
cd "$(dirname "$0")"

SNARKJS="$(pwd)/../../node_modules/.bin/snarkjs"
NODE_MODULES="$(pwd)/../../node_modules"
PTAU_POWER=12 # supports up to 2^12 constraints; this circuit uses ~1300

rm -rf build
mkdir build
circom diploma_membership.circom --r1cs --wasm --sym -o build -l "$NODE_MODULES"

# generate_witness.js is CommonJS; client-lib's package.json is ESM.
echo '{"type": "commonjs"}' > build/diploma_membership_js/package.json

cd build
"$SNARKJS" powersoftau new bn128 "$PTAU_POWER" pot_0000.ptau -v
"$SNARKJS" powersoftau contribute pot_0000.ptau pot_0001.ptau --name="zk-puoi toy setup" -v -e="$(head -c 64 /dev/urandom | base64)"
"$SNARKJS" powersoftau prepare phase2 pot_0001.ptau pot_final.ptau -v

"$SNARKJS" groth16 setup diploma_membership.r1cs pot_final.ptau diploma_membership_0000.zkey
"$SNARKJS" zkey contribute diploma_membership_0000.zkey diploma_membership_final.zkey --name="zk-puoi toy setup" -v -e="$(head -c 64 /dev/urandom | base64)"
"$SNARKJS" zkey export verificationkey diploma_membership_final.zkey verification_key.json

rm -f pot_0000.ptau pot_0001.ptau pot_final.ptau diploma_membership_0000.zkey

echo "Built build/diploma_membership_js/diploma_membership.wasm, build/diploma_membership_final.zkey, build/verification_key.json"
