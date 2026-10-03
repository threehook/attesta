#!/usr/bin/env bash
# Generates a real proof for a given private x, using the committed build/ artifacts (run build.sh first if build/ doesn't exist yet). Prints
# proof.json and public.json so they can be pasted straight into a POST /v1/authorize body's "proof" and "publicSignals" fields. This is the
# client-side stand-in for a browser-based prover, which client-lib will eventually wrap properly (see the project README).
set -euo pipefail
cd "$(dirname "$0")"

if [ $# -ne 1 ]; then
    echo "usage: $0 <x>" >&2
    exit 1
fi
X="$1"

SNARKJS="$(pwd)/../../node_modules/.bin/snarkjs"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

echo "{\"x\": \"$X\"}" >"$WORKDIR/input.json"
node build/cubic_js/generate_witness.js build/cubic_js/cubic.wasm "$WORKDIR/input.json" "$WORKDIR/witness.wtns"
"$SNARKJS" groth16 prove build/cubic_final.zkey "$WORKDIR/witness.wtns" "$WORKDIR/proof.json" "$WORKDIR/public.json" >/dev/null

echo "proof.json:"
cat "$WORKDIR/proof.json"
echo
echo "public.json:"
cat "$WORKDIR/public.json"
