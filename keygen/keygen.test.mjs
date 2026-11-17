import { test } from "node:test";
import assert from "node:assert/strict";
import { createPrivateKey, createPublicKey } from "node:crypto";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { base58btc, encodePrivateKey, peerIdFromPublicKey, generate } from "./keygen.js";

const seed = new Uint8Array(Array.from({ length: 32 }, (_, i) => i));
const expectedKey = "CAESQAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fA6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg=";
const expectedPeerId = "12D3KooWA4Xop1JaT3MHxwYMkCepYsv4iPVopMXwCz5iHYdBfeSB";
const decodedKey = Uint8Array.from(Buffer.from(expectedKey, "base64"));
const vectorPub = decodedKey.slice(36);

test("encodePrivateKey matches the test vector", () => {
  assert.equal(encodePrivateKey(seed, vectorPub), expectedKey);
});

test("peerIdFromPublicKey matches the test vector", () => {
  assert.equal(peerIdFromPublicKey(vectorPub), expectedPeerId);
});

test("base58btc handles leading zeros and empty input", () => {
  assert.equal(base58btc(new Uint8Array([0, 0, 1])), "112");
  assert.equal(base58btc(new Uint8Array([])), "");
  assert.equal(base58btc(new Uint8Array([0])), "1");
});

test("wrong lengths throw with the exact message", () => {
  const short = new Uint8Array(31);
  for (const [a, b] of [
    [new Uint8Array(31), vectorPub],
    [seed, new Uint8Array(31)],
    [short, short],
    [seed, vectorPub.slice(0, 31)],
  ]) {
    assert.throws(() => encodePrivateKey(a, b), /seed and public key must be 32 bytes/);
    assert.throws(() => peerIdFromPublicKey(b.length === 31 ? b : new Uint8Array(31)), /seed and public key must be 32 bytes/);
  }
  assert.throws(() => encodePrivateKey(seed, new Uint8Array(31)), {
    message: "seed and public key must be 32 bytes",
  });
  assert.throws(() => peerIdFromPublicKey(new Uint8Array(31)), {
    message: "seed and public key must be 32 bytes",
  });
});

test("generate produces a valid Ed25519 key and peer ID", async () => {
  const { key, peerId } = await generate();
  const decoded = Uint8Array.from(Buffer.from(key, "base64"));
  assert.equal(decoded.length, 68);
  assert.deepEqual(Array.from(decoded.slice(0, 4)), [0x08, 0x01, 0x12, 0x40]);
  assert.match(peerId, /^12D3KooW/);
  assert.equal(peerIdFromPublicKey(decoded.slice(36)), peerId);

  const derived = createPublicKey(
    createPrivateKey({
      key: Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), Buffer.from(decoded.slice(4, 36))]),
      format: "der",
      type: "pkcs8",
    }),
  ).export({ format: "der", type: "spki" });
  assert.deepEqual(Uint8Array.from(derived.subarray(derived.length - 32)), decoded.slice(36));

  const again = await generate();
  assert.notEqual(key, again.key);
});

test("index.html has the required content", () => {
  const html = readFileSync(fileURLToPath(new URL("./index.html", import.meta.url)), "utf8");
  for (const needle of ["GITPIN_KEY", "Make a key", "<title>gitpin key</title>", 'name="viewport"']) {
    assert.ok(html.includes(needle), `missing ${needle}`);
  }
});

test("vector seed derives the vector public key", () => {
  const spki = createPublicKey(
    createPrivateKey({
      key: Buffer.concat([Buffer.from("302e020100300506032b657004220420", "hex"), Buffer.from(seed)]),
      format: "der",
      type: "pkcs8",
    }),
  ).export({ format: "der", type: "spki" });
  assert.deepEqual(Uint8Array.from(spki.subarray(spki.length - 32)), vectorPub);
});
