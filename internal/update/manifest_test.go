package update_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/update"
)

// errRead stands in for a file that fails partway.
var errRead = errors.New("read failed")

// testKey returns the key pair seeded by seed, the same on every run.
func testKey(seed byte) (ed25519.PublicKey, ed25519.PrivateKey) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))

	return update.PublicKeyOf(private), private
}

// zipBody stands in for a release's zip.
const zipBody = "the zipped bundle"

// unsigned returns a manifest for zipBody at version, in form and not yet signed.
func unsigned(version string) update.Manifest {
	sum := sha512.Sum512([]byte(zipBody))

	return update.Manifest{
		Version:  version,
		Platform: update.Platform,
		File:     update.FileName(update.Platform, version),
		Size:     int64(len(zipBody)),
		SHA512:   hex.EncodeToString(sum[:]),
	}
}

// signed returns unsigned(version) signed with private.
func signed(t *testing.T, version string, private ed25519.PrivateKey) update.Manifest {
	t.Helper()

	m, err := update.Sign(unsigned(version), private)
	require.NoError(t, err)

	return m
}

func TestAssetNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, update.Assets{
		Zip:      brand.Name + "_0.3.0_darwin_universal.zip",
		DMG:      brand.Name + ".dmg",
		Sums:     "SHA256SUMS",
		Manifest: "manifest-darwin.json",
	}, update.AssetNames("0.3.0"))

	assert.Equal(t, brand.Name+"_0.2.0-rc.1_darwin_universal.zip", update.AssetNames("0.2.0-rc.1").Zip)
	assert.Equal(t, brand.Name+"_1.0.0_linux_amd64.zip", update.FileName("linux-amd64", "1.0.0"))
}

func TestValidVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		want    bool
	}{
		{version: "0.3.0", want: true},
		{version: "10.20.30", want: true},
		{version: "0.2.0-rc.1", want: true},
		{version: "1.0.0-alpha-2.x", want: true},
		{version: "0.3.0-rc.0", want: true},
		{version: "0.3.0-0", want: true},
		{version: "0.3.0-0a.1", want: true},
		{version: "0.3.0-x.7.z.92", want: true},
		{version: "0.3.0-rc.01"},
		{version: "0.3.0-01"},
		{version: "0.3.0-rc.1.00"},
		{version: "v0.3.0"},
		{version: "0.3"},
		{version: "0.3.0.1"},
		{version: "01.3.0"},
		{version: "0.3.0-"},
		{version: "0.3.0-rc..1"},
		{version: "0.3.0+build.1"},
		{version: "0.3.0\nfile x"},
		{version: ""},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, update.ValidVersion(tt.version))
		})
	}
}

func TestSignedMessage(t *testing.T) {
	t.Parallel()

	m := unsigned("0.3.0")

	assert.Equal(t, brand.Slug+" update 1\n"+
		"platform darwin-universal\n"+
		"version 0.3.0\n"+
		"file "+brand.Name+"_0.3.0_darwin_universal.zip\n"+
		"size 17\n"+
		"sha512 "+m.SHA512+"\n", string(update.SignedMessage(m)))
}

func TestSignThenVerify(t *testing.T) {
	t.Parallel()

	public, private := testKey(1)

	for _, version := range []string{"0.3.0", "0.2.0-rc.1"} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			m := signed(t, version, private)

			sig, err := base64.StdEncoding.DecodeString(m.Signature)
			require.NoError(t, err)
			assert.Len(t, sig, ed25519.SignatureSize)

			require.NoError(t, update.Verify(m, public))
		})
	}
}

func TestVerifyRefusesAChangedField(t *testing.T) {
	t.Parallel()

	public, private := testKey(1)
	other := sha512.Sum512([]byte("another zip"))
	otherSig := signed(t, "0.3.1", private).Signature

	tests := []struct {
		name   string
		change func(*update.Manifest)
		want   error
	}{
		{name: "the version, relabeled with its file", change: func(m *update.Manifest) {
			m.Version = "9.9.9"
			m.File = update.FileName(m.Platform, m.Version)
		}, want: update.ErrSignature},
		{name: "the version alone", change: func(m *update.Manifest) { m.Version = "9.9.9" }, want: update.ErrInvalid},
		{name: "the size", change: func(m *update.Manifest) { m.Size++ }, want: update.ErrSignature},
		{name: "the digest", change: func(m *update.Manifest) { m.SHA512 = hex.EncodeToString(other[:]) }, want: update.ErrSignature},
		{name: "the platform, with its file", change: func(m *update.Manifest) {
			m.Platform = "darwin-arm64"
			m.File = update.FileName(m.Platform, m.Version)
		}, want: update.ErrSignature},
		{name: "the file", change: func(m *update.Manifest) { m.File = "other.zip" }, want: update.ErrInvalid},
		{name: "the signature, cut short", change: func(m *update.Manifest) { m.Signature = m.Signature[:20] }, want: update.ErrInvalid},
		{name: "the signature, not base64", change: func(m *update.Manifest) { m.Signature = "!" + m.Signature[1:] }, want: update.ErrInvalid},
		{name: "no signature", change: func(m *update.Manifest) { m.Signature = "" }, want: update.ErrInvalid},
		{name: "the signature, another manifest's", change: func(m *update.Manifest) { m.Signature = otherSig }, want: update.ErrSignature},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := signed(t, "0.3.0", private)
			tt.change(&m)

			require.ErrorIs(t, update.Verify(m, public), tt.want)
		})
	}
}

func TestVerifyRefusesTheWrongKey(t *testing.T) {
	t.Parallel()

	_, private := testKey(1)
	other, _ := testKey(2)

	m := signed(t, "0.3.0", private)

	require.ErrorIs(t, update.Verify(m, other), update.ErrSignature)
	require.ErrorIs(t, update.Verify(m, other[:31]), update.ErrKey, "a key of the wrong length is refused, never a panic")
	require.ErrorIs(t, update.Verify(m, nil), update.ErrKey)
}

func TestSignRefuses(t *testing.T) {
	t.Parallel()

	_, private := testKey(1)

	_, err := update.Sign(unsigned("0.3.0"), private[:10])
	require.ErrorIs(t, err, update.ErrKey)

	bad := unsigned("0.3.0")
	bad.Size = 0

	_, err = update.Sign(bad, private)
	require.ErrorIs(t, err, update.ErrInvalid, "a manifest Verify would refuse is never signed")
}

func TestVerifyChecksTheForm(t *testing.T) {
	t.Parallel()

	public, private := testKey(1)

	tests := []struct {
		name   string
		change func(*update.Manifest)
		want   string
	}{
		{name: "a platform with a newline", change: func(m *update.Manifest) { m.Platform = "darwin\nversion 9.9.9" }, want: "platform"},
		{name: "an empty platform", change: func(m *update.Manifest) { m.Platform = "" }, want: "platform"},
		{name: "a version with a v", change: func(m *update.Manifest) {
			m.Version = "v0.3.0"
			m.File = update.FileName(m.Platform, m.Version)
		}, want: "version"},
		{name: "a file in another folder", change: func(m *update.Manifest) { m.File = "../" + m.File }, want: "file"},
		{name: "an empty file", change: func(m *update.Manifest) { m.Size = 0 }, want: "size"},
		{name: "a negative size", change: func(m *update.Manifest) { m.Size = -1 }, want: "size"},
		{name: "a file over the limit", change: func(m *update.Manifest) { m.Size = update.MaxFileBytes + 1 }, want: "size"},
		{name: "an uppercase digest", change: func(m *update.Manifest) { m.SHA512 = strings.ToUpper(m.SHA512) }, want: "SHA-512"},
		{name: "a short digest", change: func(m *update.Manifest) { m.SHA512 = m.SHA512[:64] }, want: "SHA-512"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := signed(t, "0.3.0", private)
			tt.change(&m)

			err := update.Verify(m, public)

			require.ErrorIs(t, err, update.ErrInvalid)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	t.Run("a file at the limit", func(t *testing.T) {
		t.Parallel()

		m := unsigned("0.3.0")
		m.Size = update.MaxFileBytes

		m, err := update.Sign(m, private)
		require.NoError(t, err)
		require.NoError(t, update.Verify(m, public))
	})
}

func TestEncodeThenParse(t *testing.T) {
	t.Parallel()

	public, private := testKey(1)
	m := signed(t, "0.3.0", private)

	body, err := update.Encode(m)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(string(body), "{\n  \"version\": \"0.3.0\",\n  \"platform\": \"darwin-universal\",\n"))
	assert.True(t, strings.HasSuffix(string(body), "}\n"))

	got, err := update.Parse(body)
	require.NoError(t, err)
	assert.Equal(t, m, got)
	require.NoError(t, update.Verify(got, public))
}

func TestParseRefuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "not JSON", body: "<html>"},
		{name: "empty", body: ""},
		{name: "two documents", body: `{"version":"0.3.0"}{"version":"0.3.1"}`},
		{name: "text after the object", body: `{"version":"0.3.0"} x`},
		{name: "a field the format doesn't have", body: `{"version":"0.3.0","url":"https://example.com/other.zip"}`},
		{name: "a size that is no number", body: `{"size":"17"}`},
		{name: "larger than the limit", body: `{"version":"0.3.0","pad":"` + strings.Repeat("x", update.MaxManifestBytes) + `"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := update.Parse([]byte(tt.body))

			require.ErrorIs(t, err, update.ErrInvalid)
		})
	}
}

func TestDigest(t *testing.T) {
	t.Parallel()

	sum := sha512.Sum512([]byte(zipBody))

	size, digest, err := update.Digest(strings.NewReader(zipBody))

	require.NoError(t, err)
	assert.Equal(t, int64(len(zipBody)), size)
	assert.Equal(t, hex.EncodeToString(sum[:]), digest)

	_, _, err = update.Digest(iotest.ErrReader(errRead))
	require.ErrorIs(t, err, errRead)
}

func TestCheckFile(t *testing.T) {
	t.Parallel()

	m := unsigned("0.3.0")

	tests := []struct {
		name string
		body string
		want string // "" when the file matches
	}{
		{name: "the file the manifest names", body: zipBody},
		{name: "one byte short", body: zipBody[1:], want: "bytes"},
		{name: "one byte more", body: zipBody + "!", want: "bytes"},
		{name: "the same size, other bytes", body: strings.ToUpper(zipBody), want: "SHA-512"},
		{name: "empty", body: "", want: "bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := update.CheckFile(m, strings.NewReader(tt.body))

			if tt.want == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, update.ErrMismatch)
			assert.Contains(t, err.Error(), tt.want)
		})
	}

	t.Run("a read that fails", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, update.CheckFile(m, iotest.ErrReader(errRead)), errRead)
	})
}

func FuzzVerify(f *testing.F) {
	public, private := testKey(1)

	m, err := update.Sign(unsigned("0.3.0"), private)
	require.NoError(f, err)

	body, err := update.Encode(m)
	require.NoError(f, err)

	f.Add(body)
	f.Add([]byte(`{"version":"0.3.0","size":-1}`))
	f.Add([]byte("{}"))

	f.Fuzz(func(t *testing.T, data []byte) {
		got, parseErr := update.Parse(data)
		if parseErr != nil {
			return
		}

		// Whatever the manifest holds, Verify answers without a panic, and only the
		// signed manifest's own fields verify.
		if update.Verify(got, public) == nil {
			assert.Equal(t, update.SignedMessage(m), update.SignedMessage(got))
		}
	})
}
