package update_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/update"
)

// pemBlock returns der as a PEM block of type kind.
func pemBlock(kind string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}))
}

// ecdsaDER returns an ECDSA key's private half in PKCS#8 and its public half in PKIX: a
// key in the right forms that isn't Ed25519.
func ecdsaDER(t *testing.T) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	private, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)

	public, err := x509.MarshalPKIXPublicKey(key.Public())
	require.NoError(t, err)

	return pemBlock("PRIVATE KEY", private), pemBlock("PUBLIC KEY", public)
}

func TestPrivateKeyRoundTrip(t *testing.T) {
	t.Parallel()

	public, private := testKey(3)

	body, err := update.EncodePrivateKey(private)
	require.NoError(t, err)

	block, rest := pem.Decode(body)
	require.NotNil(t, block)
	assert.Equal(t, "PRIVATE KEY", block.Type, "PKCS#8")
	assert.Empty(t, rest)

	got, err := update.ParsePrivateKey(body)
	require.NoError(t, err)
	assert.True(t, private.Equal(got))

	assert.True(t, public.Equal(update.PublicKeyOf(got)))

	_, err = update.ParseKey(body)
	require.ErrorIs(t, err, update.ErrKey, "a private key is read only by ParsePrivateKey")
}

func TestParseKey(t *testing.T) {
	t.Parallel()

	public, _ := testKey(4)

	pkix, err := x509.MarshalPKIXPublicKey(public)
	require.NoError(t, err)

	for name, text := range map[string]string{
		"raw base64, as ReleaseKey holds it": update.EncodeKey(public),
		"raw base64 with a newline":          update.EncodeKey(public) + "\n",
		"a PKIX PEM block":                   pemBlock("PUBLIC KEY", pkix),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, parseErr := update.ParseKey([]byte(text))

			require.NoError(t, parseErr)
			assert.True(t, public.Equal(got))
		})
	}
}

func TestParseKeyRefusesAMalformedKey(t *testing.T) {
	t.Parallel()

	public, private := testKey(4)
	ecPrivate, ecPublic := ecdsaDER(t)
	pkcs8, err := update.EncodePrivateKey(private)
	require.NoError(t, err)

	tests := map[string]string{
		"empty":                     "",
		"not base64":                "not a key!",
		"31 bytes":                  base64.StdEncoding.EncodeToString(public[:31]),
		"33 bytes":                  base64.StdEncoding.EncodeToString(append(public, 0)),
		"URL-safe base64":           strings.NewReplacer("+", "-", "/", "_").Replace(update.EncodeKey(public)) + "-_",
		"a PEM block cut short":     strings.Split(pemBlock("PUBLIC KEY", []byte("der")), "\n")[0] + "\nAAAA\n",
		"a certificate":             pemBlock("CERTIFICATE", []byte("der")),
		"an ECDSA public key":       ecPublic,
		"an ECDSA private key":      ecPrivate,
		"a PKIX block of garbage":   pemBlock("PUBLIC KEY", []byte("garbage")),
		"a PKCS#8 block of garbage": pemBlock("PRIVATE KEY", []byte("garbage")),
		"two PEM blocks":            string(pkcs8) + string(pkcs8),
		"an encrypted key's headers": string(pem.EncodeToMemory(&pem.Block{
			Type: "PRIVATE KEY", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte("der"),
		})),
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, parseErr := update.ParseKey([]byte(text))

			require.ErrorIs(t, parseErr, update.ErrKey)
		})
	}
}

func TestParsePrivateKeyRefuses(t *testing.T) {
	t.Parallel()

	public, private := testKey(5)
	ecPrivate, _ := ecdsaDER(t)

	valid, err := update.EncodePrivateKey(private)
	require.NoError(t, err)

	pkix, err := x509.MarshalPKIXPublicKey(public)
	require.NoError(t, err)

	tests := map[string]string{
		"a public key":            pemBlock("PUBLIC KEY", pkix),
		"raw base64":              update.EncodeKey(public),
		"an ECDSA private key":    ecPrivate,
		"a block of garbage":      pemBlock("PRIVATE KEY", []byte("garbage")),
		"text after the block":    string(valid) + "more",
		"an OpenSSH private key":  pemBlock("OPENSSH PRIVATE KEY", []byte("der")),
		"no block, only the name": "PRIVATE KEY",
	}

	for name, text := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, parseErr := update.ParsePrivateKey([]byte(text))

			require.ErrorIs(t, parseErr, update.ErrKey)
		})
	}
}

func TestEncodePrivateKeyRefusesAMalformedKey(t *testing.T) {
	t.Parallel()

	_, err := update.EncodePrivateKey(make([]byte, 10))

	require.ErrorIs(t, err, update.ErrKey)
	assert.Nil(t, update.PublicKeyOf(make([]byte, 10)), "a key of the wrong length has no public half, never a panic")
}

// smallOrderKeys are encodings of Ed25519 points of small order: canonical, with the sign
// bit set, and non-canonical (y = p and y = p + 1).
func smallOrderKeys() map[string][]byte {
	field := func(low, top byte) []byte {
		b := bytes.Repeat([]byte{0xff}, ed25519.PublicKeySize)
		b[0], b[31] = low, top

		return b
	}

	order8 := []byte{
		0x26, 0xe8, 0x95, 0x8f, 0xc2, 0xb2, 0x27, 0xb0, 0x45, 0xc3, 0xf4, 0x89, 0xf2, 0xef, 0x98, 0xf0,
		0xd5, 0xdf, 0xac, 0x05, 0xd3, 0xc6, 0x33, 0x39, 0xb1, 0x38, 0x02, 0x88, 0x6d, 0x53, 0xfc, 0x05,
	}
	order8Negated := append(bytes.Clone(order8[:31]), order8[31]|0x80)

	return map[string][]byte{
		"zero (order 4)":               make([]byte, ed25519.PublicKeySize),
		"zero with the sign bit":       append(make([]byte, 31), 0x80),
		"the identity":                 append([]byte{1}, make([]byte, 31)...),
		"an order-8 point":             order8,
		"an order-8 point, other sign": order8Negated,
		"p - 1 (order 2)":              field(0xec, 0x7f),
		"p, non-canonical zero":        field(0xed, 0x7f),
		"p + 1, non-canonical one":     field(0xee, 0x7f),
		"p + 1 with the sign bit":      field(0xee, 0xff),
	}
}

func TestParseKeyRefusesASmallOrderPoint(t *testing.T) {
	t.Parallel()

	for name, key := range smallOrderKeys() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := update.ParseKey([]byte(base64.StdEncoding.EncodeToString(key)))
			require.ErrorIs(t, err, update.ErrKey)

			pkix, err := x509.MarshalPKIXPublicKey(ed25519.PublicKey(key))
			require.NoError(t, err)

			_, err = update.ParseKey([]byte(pemBlock("PUBLIC KEY", pkix)))
			require.ErrorIs(t, err, update.ErrKey, "as a PKIX block too")
		})
	}
}

func TestVerifyRefusesAForgeryUnderTheIdentity(t *testing.T) {
	t.Parallel()

	identity := ed25519.PublicKey(smallOrderKeys()["the identity"])

	// R = the identity and S = 0 satisfy [S]B = R + [k]A for A = the identity, whatever
	// the message: no private key signed this.
	forged := unsigned("9.9.9")

	sig := append(bytes.Clone(identity), make([]byte, 32)...)
	forged.Signature = base64.StdEncoding.EncodeToString(sig)

	require.True(t, ed25519.Verify(identity, update.SignedMessage(forged), sig),
		"the forgery verifies with the bare primitive, so the guard is what stops it")
	require.ErrorIs(t, update.Verify(forged, identity), update.ErrKey)
}
