pragma circom 2.0.0;

// Cubic proves knowledge of a private x such that x^3 + x + 5 == y, with y public. This is a circom port of backend/internal/proof's gnark
// CubicCircuit, so the two toolchains' toy circuits match: x stays private (a plain `signal input` is private unless declared public on component
// main), and y is public because circom/snarkjs outputs are always part of the proof's public signals.
template Cubic() {
    signal input x;
    signal output y;

    signal x2;
    signal x3;

    x2 <== x * x;
    x3 <== x2 * x;

    y <== x3 + x + 5;
}

component main = Cubic();
