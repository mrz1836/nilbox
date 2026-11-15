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
// ReleaseKey holds it, or a PEM block, a public key (PKIX) or a private key (PKCS#8),
// whose public half it returns. Space around the text is ignored.
func ParseKey(text []byte) (ed25519.PublicKey, error) {
	text = bytes.TrimSpace(text)

	if bytes.HasPrefix(text, []byte("-----BEGIN ")) {
		return publicKeyFromPEM(text)
	}

	raw, err := base64.StdEncoding.Strict().DecodeString(string(text))
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: not %d bytes of base64, or a PEM block", ErrKey, ed25519.PublicKeySize)
	}

	return ed25519.PublicKey(raw), nil
}

// publicKeyFromPEM reads the public key of the one PEM block in text, a public key
// (PKIX) or a private key (PKCS#8).
func publicKeyFromPEM(text []byte) (ed25519.PublicKey, error) {
	block, err := onePEMBlock(text)
	if err != nil {
		return nil, err
	}

	switch block.Type {
	case pemPrivateKey:
		key, keyErr := parsePKCS8(block.Bytes)
		if keyErr != nil {
			return nil, keyErr
		}

		return PublicKeyOf(key), nil
	case pemPublicKey:
		key, keyErr := x509.ParsePKIXPublicKey(block.Bytes)
		if keyErr != nil {
			return nil, fmt.Errorf("%w: %w", ErrKey, keyErr)
		}

		public, ok := key.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("%w: the PEM block holds a %T", ErrKey, key)
		}

		return public, nil
	default:
		return nil, fmt.Errorf("%w: a PEM block of type %q", ErrKey, block.Type)
	}
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
