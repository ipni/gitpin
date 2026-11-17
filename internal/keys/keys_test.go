package keys

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"golang.org/x/crypto/ssh"
)

const (
	vectorKey    = "CAESQAABAgMEBQYHCAkKCwwNDg8QERITFBUWFxgZGhscHR4fA6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg="
	vectorPeerID = "12D3KooWA4Xop1JaT3MHxwYMkCepYsv4iPVopMXwCz5iHYdBfeSB"
)

func seedKey(t *testing.T) crypto.PrivKey {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	k, err := crypto.UnmarshalEd25519PrivateKey(ed25519.NewKeyFromSeed(seed[:]))
	if err != nil {
		t.Fatalf("UnmarshalEd25519PrivateKey: %v", err)
	}
	return k
}

func assertPeerID(t *testing.T, k crypto.PrivKey, want string) {
	t.Helper()
	id, err := peer.IDFromPrivateKey(k)
	if err != nil {
		t.Fatalf("IDFromPrivateKey: %v", err)
	}
	if id.String() != want {
		t.Fatalf("peer ID = %s, want %s", id, want)
	}
}

func TestParseVector(t *testing.T) {
	k, err := Parse(vectorKey)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	assertPeerID(t, k, vectorPeerID)

	k2, err := Parse("  " + vectorKey + "\n")
	if err != nil {
		t.Fatalf("Parse padded: %v", err)
	}
	if !k.Equals(k2) {
		t.Fatal("padded parse does not equal vector key")
	}
}

func TestSeedKeyMarshalsToVector(t *testing.T) {
	raw, err := crypto.MarshalPrivateKey(seedKey(t))
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	got := base64.StdEncoding.EncodeToString(raw)
	if got != vectorKey {
		t.Fatalf("marshalled = %s, want %s", got, vectorKey)
	}
}

func TestGenerate(t *testing.T) {
	k1, s1, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if k1.Type() != crypto.Ed25519 {
		t.Fatalf("key type = %v, want Ed25519", k1.Type())
	}
	raw, err := crypto.MarshalPrivateKey(k1)
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	if base64.StdEncoding.EncodeToString(raw) != s1 {
		t.Fatal("marshalled key does not equal returned string")
	}
	parsed, err := Parse(s1)
	if err != nil {
		t.Fatalf("Parse(Generate): %v", err)
	}
	if !k1.Equals(parsed) {
		t.Fatal("Parse(Generate) does not round-trip")
	}
	k2, s2, err := Generate()
	if err != nil {
		t.Fatalf("Generate second: %v", err)
	}
	if s1 == s2 || k1.Equals(k2) {
		t.Fatal("two Generate calls produced the same key")
	}
}

func TestParseErrors(t *testing.T) {
	raw, err := base64.StdEncoding.DecodeString(vectorKey)
	if err != nil {
		t.Fatalf("decode vector: %v", err)
	}
	raw[len(raw)-1] ^= 0x01
	wrongPub := base64.StdEncoding.EncodeToString(raw)

	secp, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateSecp256k1Key: %v", err)
	}
	secpRaw, err := crypto.MarshalPrivateKey(secp)
	if err != nil {
		t.Fatalf("MarshalPrivateKey(secp): %v", err)
	}
	wrongLen := base64.StdEncoding.EncodeToString([]byte{0x08, 0x01, 0x12, 0x04, 0x01, 0x02, 0x03, 0x04})

	cases := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", errEmpty},
		{"whitespace", "  \n", errEmpty},
		{"not base64", "not base64!", errBadKey},
		{"base64 hello", base64.StdEncoding.EncodeToString([]byte("hello")), errBadKey},
		{"secp256k1", base64.StdEncoding.EncodeToString(secpRaw), errBadKey},
		{"wrong public half", string(wrongPub), errBadKey},
		{"wrong length", wrongLen, errBadKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k, err := Parse(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want errors.Is %v", err, tc.want)
			}
			if k != nil {
				t.Fatal("key is not nil on error")
			}
			if len(tc.in) > 0 && bytes.Contains([]byte(err.Error()), []byte(tc.in)) {
				t.Fatalf("error %q contains input", err)
			}
			if err.Error() != tc.want.Error() {
				t.Fatalf("message = %q, want %q", err.Error(), tc.want.Error())
			}
		})
	}
}

func TestParseNinetySixByteKey(t *testing.T) {
	k, err := Parse(base64.StdEncoding.EncodeToString(protoKey(seedKey96(t))))
	if err != nil {
		t.Fatalf("Parse 96-byte: %v", err)
	}
	assertPeerID(t, k, vectorPeerID)
}

func seedKey96(t *testing.T) []byte {
	t.Helper()
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(i)
	}
	pub := ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey)
	return append(append(append([]byte{}, seed[:]...), pub...), pub...)
}

func protoKey(data []byte) []byte {
	buf := make([]byte, 0, len(data)+2)
	buf = append(buf, 0x08, 0x01)
	buf = append(buf, 0x12, byte(len(data)))
	return append(buf, data...)
}

func TestFromEnv(t *testing.T) {
	t.Setenv(EnvVar, vectorKey)
	k, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	assertPeerID(t, k, vectorPeerID)

	t.Setenv(EnvVar, "")
	if _, err = FromEnv(); !errors.Is(err, errEmpty) {
		t.Fatalf("empty env: err = %v, want %v", err, errEmpty)
	}

	t.Setenv(EnvVar, "x")
	os.Unsetenv(EnvVar)
	if _, err = FromEnv(); !errors.Is(err, errEmpty) {
		t.Fatalf("unset env: err = %v, want %v", err, errEmpty)
	}
}

func seedBytes() []byte {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}
	return seed
}

func TestParseOpenSSH(t *testing.T) {
	block, err := ssh.MarshalPrivateKey(ed25519.NewKeyFromSeed(seedBytes()), "gitpin")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(block)

	k, err := Parse(string(pemBytes))
	if err != nil {
		t.Fatalf("Parse OpenSSH: %v", err)
	}
	assertPeerID(t, k, vectorPeerID)

	k2, err := Parse("  \n" + string(pemBytes) + "\r\n  ")
	if err != nil {
		t.Fatalf("Parse OpenSSH padded: %v", err)
	}
	assertPeerID(t, k2, vectorPeerID)

	crlf := strings.ReplaceAll(string(pemBytes), "\n", "\r\n")
	k3, err := Parse(crlf)
	if err != nil {
		t.Fatalf("Parse OpenSSH CRLF: %v", err)
	}
	assertPeerID(t, k3, vectorPeerID)
}

func TestParsePKCS8(t *testing.T) {
	der, err := x509.MarshalPKCS8PrivateKey(ed25519.NewKeyFromSeed(seedBytes()))
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	k, err := Parse(string(pemBytes))
	if err != nil {
		t.Fatalf("Parse PKCS8: %v", err)
	}
	assertPeerID(t, k, vectorPeerID)
}

func TestParsePassphrase(t *testing.T) {
	block, err := ssh.MarshalPrivateKeyWithPassphrase(ed25519.NewKeyFromSeed(seedBytes()), "gitpin", []byte("pw"))
	if err != nil {
		t.Fatalf("MarshalPrivateKeyWithPassphrase: %v", err)
	}
	pemBytes := pem.EncodeToMemory(block)

	k, err := Parse(string(pemBytes))
	if err == nil {
		t.Fatal("expected error for passphrase-protected key")
	}
	if k != nil {
		t.Fatal("key is not nil on error")
	}
	if !strings.Contains(err.Error(), "passphrase") {
		t.Fatalf("err = %v, want it to mention passphrase", err)
	}
}

func TestParseNotEd25519(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	block, err := ssh.MarshalPrivateKey(key, "gitpin")
	if err != nil {
		t.Fatalf("MarshalPrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(block)

	k, err := Parse(string(pemBytes))
	if !errors.Is(err, errBadKey) {
		t.Fatalf("err = %v, want %v", err, errBadKey)
	}
	if k != nil {
		t.Fatal("key is not nil on error")
	}
}

func TestParseGarbagePEM(t *testing.T) {
	in := "-----BEGIN OPENSSH PRIVATE KEY-----\nnot-a-real-key\n-----END OPENSSH PRIVATE KEY-----\n"
	k, err := Parse(in)
	if !errors.Is(err, errBadKey) {
		t.Fatalf("err = %v, want %v", err, errBadKey)
	}
	if k != nil {
		t.Fatal("key is not nil on error")
	}
}
