package update

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
)

// ReleaseKey is the Ed25519 public key, base64, that every release's manifest must
// verify with: the trust root of the app's updates. It stays empty until the release key
// is made and its public half pinned here, and magex app:package refuses to package a
// release while it is, so no release ever ships a manifest the app couldn't verify.
const ReleaseKey = ""

// PEM block types of the key forms this package reads and writes.
const (
	pemPrivateKey = "PRIVATE KEY" // PKCS#8
	pemPublicKey  = "PUBLIC KEY"  // PKIX
)

// ParseKey reads an Ed25519 public key from text: the raw 32 bytes in base64, as
// ReleaseKey holds it, or a PEM block of a public key (PKIX). Space around the text is
// ignored. It refuses a private key, which only ParsePrivateKey reads, and a point of
// small order, under which a forged signature verifies for any message.
func ParseKey(text []byte) (ed25519.PublicKey, error) {
	text = bytes.TrimSpace(text)

	var key ed25519.PublicKey

	if bytes.HasPrefix(text, []byte("-----BEGIN ")) {
		pkix, err := publicKeyFromPEM(text)
		if err != nil {
			return nil, err
		}

		key = pkix
	} else {
		raw, err := base64.StdEncoding.Strict().DecodeString(string(text))
		if err != nil {
			return nil, fmt.Errorf("%w: not base64, or a PEM block", ErrKey)
		}

		key = raw
	}

	if err := checkPublicKey(key); err != nil {
		return nil, err
	}

	return key, nil
}

// publicKeyFromPEM reads the public key (PKIX) in the one PEM block in text.
func publicKeyFromPEM(text []byte) (ed25519.PublicKey, error) {
	block, err := onePEMBlock(text)
	if err != nil {
		return nil, err
	}

	if block.Type != pemPublicKey {
		return nil, fmt.Errorf("%w: a PEM block of type %q, not %q", ErrKey, block.Type, pemPublicKey)
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKey, err)
	}

	public, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: the PEM block holds a %T", ErrKey, key)
	}

	return public, nil
}

// checkPublicKey fails with ErrKey unless key is 32 bytes and not a point of small order.
func checkPublicKey(key ed25519.PublicKey) error {
	switch {
	case len(key) != ed25519.PublicKeySize:
		return fmt.Errorf("%w: a public key is %d bytes, not %d", ErrKey, ed25519.PublicKeySize, len(key))
	case smallOrder(key):
		return fmt.Errorf("%w: a point of small order, under which any signature could be forged", ErrKey)
	}

	return nil
}

// smallOrder reports whether key encodes one of the eight Ed25519 points of small order
// (1, 2, 4, or 8), canonically or not. A signature checked with such a key can be forged
// without any private key: the identity, for one, accepts R = identity and S = 0 for
// every message. It compares the y-coordinate alone, without the sign bit, as libsodium
// does, so each point's two signs and its non-canonical encodings (y = p and y = p+1) are
// all caught.
func smallOrder(key ed25519.PublicKey) bool {
	for _, point := range smallOrderPoints() {
		var diff byte

		for i := range 31 {
			diff |= key[i] ^ point[i]
		}

		diff |= (key[31] & 0x7f) ^ point[31]

		if diff == 0 {
			return true
		}
	}

	return false
}

// smallOrderPoints are the y-coordinates, little-endian with the sign bit clear, of the
// Ed25519 points of small order: 0 (order 4), 1 (the identity), the two of order 8, p-1
// (order 2), and the non-canonical p (0) and p+1 (1). The list is libsodium's.
func smallOrderPoints() [][32]byte {
	return [][32]byte{
		{},
		{0x01},
		{
			0x26, 0xe8, 0x95, 0x8f, 0xc2, 0xb2, 0x27, 0xb0, 0x45, 0xc3, 0xf4, 0x89, 0xf2, 0xef, 0x98, 0xf0,
			0xd5, 0xdf, 0xac, 0x05, 0xd3, 0xc6, 0x33, 0x39, 0xb1, 0x38, 0x02, 0x88, 0x6d, 0x53, 0xfc, 0x05,
		},
		{
			0xc7, 0x17, 0x6a, 0x70, 0x3d, 0x4d, 0xd8, 0x4f, 0xba, 0x3c, 0x0b, 0x76, 0x0d, 0x10, 0x67, 0x0f,
			0x2a, 0x20, 0x53, 0xfa, 0x2c, 0x39, 0xcc, 0xc6, 0x4e, 0xc7, 0xfd, 0x77, 0x92, 0xac, 0x03, 0x7a,
		},
		fieldNear(0xec), // p-1
		fieldNear(0xed), // p
		fieldNear(0xee), // p+1
	}
}

// fieldNear returns the 32 little-endian bytes of p + (low - 0xed), where p = 2^255 - 19
// is 0xed, then thirty 0xff, then 0x7f.
func fieldNear(low byte) [32]byte {
	var b [32]byte

	b[0] = low

	for i := 1; i < 31; i++ {
		b[i] = 0xff
	}

	b[31] = 0x7f

	return b
}

// ParsePrivateKey reads an Ed25519 private key from a PEM block in PKCS#8, the form
// EncodePrivateKey writes. Space around the block is ignored, anything else is refused.
func ParsePrivateKey(text []byte) (ed25519.PrivateKey, error) {
	block, err := onePEMBlock(bytes.TrimSpace(text))
	if err != nil {
		return nil, err
	}

	if block.Type != pemPrivateKey {
		return nil, fmt.Errorf("%w: a PEM block of type %q, not %q", ErrKey, block.Type, pemPrivateKey)
	}

	return parsePKCS8(block.Bytes)
}

// PublicKeyOf returns the public half of key, or nil for a key of the wrong length.
func PublicKeyOf(key ed25519.PrivateKey) ed25519.PublicKey {
	if len(key) != ed25519.PrivateKeySize {
		return nil
	}

	public, _ := key.Public().(ed25519.PublicKey) // an ed25519.PrivateKey's public half always is one

	return public
}

// EncodeKey writes key as ReleaseKey holds it: its raw 32 bytes in base64.
func EncodeKey(key ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(key)
}

// EncodePrivateKey writes key as a PEM block in PKCS#8, which ParsePrivateKey reads.
func EncodePrivateKey(key ed25519.PrivateKey) ([]byte, error) {
	if len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("%w: a private key is %d bytes, not %d", ErrKey, ed25519.PrivateKeySize, len(key))
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKey, err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: pemPrivateKey, Bytes: der}), nil
}

// onePEMBlock decodes text as exactly one PEM block with no headers, which an encrypted
// key would carry.
func onePEMBlock(text []byte) (*pem.Block, error) {
	block, rest := pem.Decode(text)

	switch {
	case block == nil:
		return nil, fmt.Errorf("%w: not a PEM block", ErrKey)
	case len(bytes.TrimSpace(rest)) > 0:
		return nil, fmt.Errorf("%w: more than one PEM block", ErrKey)
	case len(block.Headers) > 0:
		return nil, fmt.Errorf("%w: the PEM block has headers, as an encrypted key would", ErrKey)
	}

	return block, nil
}

// parsePKCS8 reads der as a PKCS#8 private key that must be Ed25519.
func parsePKCS8(der []byte) (ed25519.PrivateKey, error) {
	key, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKey, err)
	}

	private, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: the PEM block holds a %T", ErrKey, key)
	}

	return private, nil
}
