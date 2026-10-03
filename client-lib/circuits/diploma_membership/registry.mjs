// Generates the fixed demo credential registry: a small set of example credentials, Poseidon-hashed into leaves of a depth-3 Merkle tree, with the
// root and each credential's private witness data (including its Merkle path) written to build/registry.json. This is a stand-in for real credential
// issuance — see the project README for why (lightweight mock VCs, no DID/signature infra).
import { buildPoseidon } from "circomlibjs";
import { writeFileSync } from "node:fs";

const DEPTH = 3;
const NUM_LEAVES = 1 << DEPTH;

// type/issuer/subject are encoded as field elements by packing their UTF-8 bytes big-endian (see backend/internal/authz's decodeFieldString, which
// must invert this exactly).
function encodeString(s) {
  return BigInt("0x" + Buffer.from(s, "utf8").toString("hex"));
}

const CREDENTIALS = [
  { id: "diploma-trusted", type: "Diploma", issuer: "trusted-university", subject: "alice", salt: "11111" },
  { id: "diploma-mill", type: "Diploma", issuer: "diploma-mill", subject: "bob", salt: "22222" },
];

async function main() {
  const poseidon = await buildPoseidon();
  const F = poseidon.F;
  const toStr = (x) => F.toString(x);

  const leaves = CREDENTIALS.map((c) => {
    const credType = encodeString(c.type);
    const issuer = encodeString(c.issuer);
    const subject = encodeString(c.subject);
    const salt = BigInt(c.salt);
    const leaf = BigInt(toStr(poseidon([credType, issuer, subject, salt])));
    return { ...c, credType, issuer, subject, salt, leaf };
  });
  while (leaves.length < NUM_LEAVES) {
    leaves.push({ id: `empty-${leaves.length}`, leaf: 0n });
  }

  let level = leaves.map((l) => l.leaf);
  const levels = [level];
  for (let d = 0; d < DEPTH; d++) {
    const next = [];
    for (let i = 0; i < level.length; i += 2) {
      next.push(BigInt(toStr(poseidon([level[i], level[i + 1]]))));
    }
    levels.push(next);
    level = next;
  }
  const root = level[0].toString();

  function pathFor(leafIndex) {
    const pathElements = [];
    const pathIndices = [];
    let idx = leafIndex;
    for (let d = 0; d < DEPTH; d++) {
      const siblingIdx = idx % 2 === 0 ? idx + 1 : idx - 1;
      pathElements.push(levels[d][siblingIdx].toString());
      pathIndices.push(idx % 2);
      idx = Math.floor(idx / 2);
    }
    return { pathElements, pathIndices };
  }

  const registry = {
    depth: DEPTH,
    root,
    credentials: CREDENTIALS.map((c, i) => {
      const { pathElements, pathIndices } = pathFor(i);
      return {
        id: c.id,
        type: c.type,
        issuer: c.issuer,
        subject: c.subject,
        privateInput: {
          credType: leaves[i].credType.toString(),
          issuer: leaves[i].issuer.toString(),
          subject: leaves[i].subject.toString(),
          salt: leaves[i].salt.toString(),
          pathElements,
          pathIndices,
        },
      };
    }),
  };

  writeFileSync(new URL("./build/registry.json", import.meta.url), JSON.stringify(registry, null, 2) + "\n");
  console.log("wrote build/registry.json, root =", root);
}

main();
