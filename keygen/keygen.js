const B58_ALPHABET = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz";

export function base58btc(bytes) {
  if (bytes.length === 0) return "";
  let zeros = 0;
  while (zeros < bytes.length && bytes[zeros] === 0) zeros++;
  const digits = [0];
  for (let i = zeros; i < bytes.length; i++) {
    let carry = bytes[i];
    for (let j = 0; j < digits.length; j++) {
      carry += digits[j] * 256;
      digits[j] = carry % 58;
      carry = Math.floor(carry / 58);
    }
    while (carry > 0) {
      digits.push(carry % 58);
      carry = Math.floor(carry / 58);
    }
  }
  let i = digits.length - 1;
  while (i >= 0 && digits[i] === 0) i--;
  let out = "";
  for (; i >= 0; i--) out += B58_ALPHABET[digits[i]];
  for (let z = 0; z < zeros; z++) out = "1" + out;
  return out;
}

function checkLen(seed, pub) {
  if (seed.length !== 32 || pub.length !== 32) {
    throw new Error("seed and public key must be 32 bytes");
  }
}

function toBase64(bytes) {
  return btoa(String.fromCharCode(...bytes));
}

export function encodePrivateKey(seed, pub) {
  checkLen(seed, pub);
  const out = new Uint8Array(68);
  out[0] = 0x08;
  out[1] = 0x01;
  out[2] = 0x12;
  out[3] = 0x40;
  out.set(seed, 4);
  out.set(pub, 36);
  return toBase64(out);
}

export function peerIdFromPublicKey(pub) {
  if (pub.length !== 32) {
    throw new Error("seed and public key must be 32 bytes");
  }
  const out = new Uint8Array(38);
  out[0] = 0x00;
  out[1] = 0x24;
  out[2] = 0x08;
  out[3] = 0x01;
  out[4] = 0x12;
  out[5] = 0x20;
  out.set(pub, 6);
  return base58btc(out);
}

export async function generate() {
  let keyPair;
  try {
    keyPair = await globalThis.crypto.subtle.generateKey(
      { name: "Ed25519" },
      true,
      ["sign", "verify"],
    );
  } catch (err) {
    throw new Error(
      "This browser cannot make Ed25519 keys. Use a current Chrome, Firefox or Safari, or run: gitpin keygen",
    );
  }
  const pkcs8 = new Uint8Array(await globalThis.crypto.subtle.exportKey("pkcs8", keyPair.privateKey));
  const pub = new Uint8Array(await globalThis.crypto.subtle.exportKey("raw", keyPair.publicKey));
  const seed = pkcs8.slice(16, 48);
  return { key: encodePrivateKey(seed, pub), peerId: peerIdFromPublicKey(pub) };
}
