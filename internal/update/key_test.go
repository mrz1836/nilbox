package update_test

import (
	"crypto/ecdsa"
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

	fromFile, err := update.ParseKey(body)
	require.NoError(t, err)
	assert.True(t, public.Equal(fromFile), "ParseKey reads a private key's public half")
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

	public, _ := testKey(5)
	ecPrivate, _ := ecdsaDER(t)

	pkix, err := x509.MarshalPKIXPublicKey(public)
	require.NoError(t, err)

	tests := map[string]string{
		"a public key":            pemBlock("PUBLIC KEY", pkix),
		"raw base64":              update.EncodeKey(public),
		"an ECDSA private key":    ecPrivate,
		"a block of garbage":      pemBlock("PRIVATE KEY", []byte("garbage")),
		"text after the block":    pemBlock("PRIVATE KEY", []byte("garbage")) + "more",
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
