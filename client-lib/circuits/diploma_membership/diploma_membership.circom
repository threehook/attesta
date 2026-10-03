pragma circom 2.0.0;

include "circomlib/circuits/poseidon.circom";
include "circomlib/circuits/switcher.circom";

// DiplomaMembership proves knowledge of a credential (credType, issuer, subject, salt) that is both a member of a fixed Merkle tree of registered
// credentials (root public) and of the required type/issuer (also public, and equal to the private credType/issuer by constraint below) — the only
// two facts disclosed about it. subject, salt, and which leaf of the tree it is all stay private: the proof shows "some registered credential of this
// type/issuer exists and I hold it" without saying which one.
template DiplomaMembership(depth) {
    signal input credType;
    signal input issuer;
    signal input subject;
    signal input salt;
    signal input pathElements[depth];
    signal input pathIndices[depth];

    signal input root;
    signal input reqType;
    signal input reqIssuer;

    credType === reqType;
    issuer === reqIssuer;

    component leafHasher = Poseidon(4);
    leafHasher.inputs[0] <== credType;
    leafHasher.inputs[1] <== issuer;
    leafHasher.inputs[2] <== subject;
    leafHasher.inputs[3] <== salt;

    signal levelHashes[depth + 1];
    levelHashes[0] <== leafHasher.out;

    component switchers[depth];
    component hashers[depth];
    for (var i = 0; i < depth; i++) {
        // pathIndices[i] selects which side of the pair the running hash sits on at this level; it must be binary, or a malicious prover
        // could use Switcher's linear combination to fake a path to any root.
        pathIndices[i] * (1 - pathIndices[i]) === 0;

        switchers[i] = Switcher();
        switchers[i].sel <== pathIndices[i];
        switchers[i].L <== levelHashes[i];
        switchers[i].R <== pathElements[i];

        hashers[i] = Poseidon(2);
        hashers[i].inputs[0] <== switchers[i].outL;
        hashers[i].inputs[1] <== switchers[i].outR;
        levelHashes[i + 1] <== hashers[i].out;
    }

    levelHashes[depth] === root;
}

component main {public [root, reqType, reqIssuer]} = DiplomaMembership(3);
