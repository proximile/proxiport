//ISC License
//
//Copyright 2023 Filippo Valsorda
//
//Permission to use, copy, modify, and/or distribute this software for any
//purpose with or without fee is hereby granted, provided that the above
//copyright notice and this permission notice appear in all copies.
//
//THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
//WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
//MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
//ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
//WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
//ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
//OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package backwardskey_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha512"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/hkdf"

	"github.com/proximile/proxiport/share/backwardskey"
)

func testAllCurves(t *testing.T, f func(*testing.T, elliptic.Curve)) {
	tests := []struct {
		name  string
		curve elliptic.Curve
	}{
		{"P256", elliptic.P256()},
		{"P384", elliptic.P384()},
		{"P521", elliptic.P521()},
	}
	for _, test := range tests {
		curve := test.curve
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f(t, curve)
		})
	}
}

func TestECDSALegacy(t *testing.T) {
	if !strings.HasPrefix(runtime.Version(), "go1.19") {
		t.Skip()
	}
	testAllCurves(t, testECDSALegacy)
}

func testECDSALegacy(t *testing.T, c elliptic.Curve) {
	r := hkdf.New(sha512.New, []byte("test"), nil, nil)
	expected, err := ecdsa.GenerateKey(c, r)
	if err != nil {
		t.Fatal(err)
	}

	r = hkdf.New(sha512.New, []byte("test"), nil, nil)
	got, err := backwardskey.ECDSALegacy(c, r)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(expected, got) {
		t.Error("Go 1.19's GenerateKey disagrees with ECDSALegacy")
	}
}

// TestECDSADeterministic exercises the bigmod-based ECDSA derivation directly
// (TestECDSALegacy is skipped off go1.19). It asserts the two properties the
// derivation must hold: a fixed secret yields the identical scalar, and that
// scalar lies in (0, N). This also confirms bigmod.NewModulus accepts each
// curve's order at runtime.
func TestECDSADeterministic(t *testing.T) {
	testAllCurves(t, func(t *testing.T, c elliptic.Curve) {
		secret := []byte("backwardskey deterministic test secret")

		k1, err := backwardskey.ECDSA(c, secret)
		if err != nil {
			t.Fatal(err)
		}
		k2, err := backwardskey.ECDSA(c, secret)
		if err != nil {
			t.Fatal(err)
		}
		if k1.D.Cmp(k2.D) != 0 {
			t.Error("ECDSA is not deterministic for a fixed secret")
		}
		if k1.D.Sign() <= 0 || k1.D.Cmp(c.Params().N) >= 0 {
			t.Error("derived scalar is not in the range (0, N)")
		}

		other, err := backwardskey.ECDSA(c, append([]byte("x"), secret...))
		if err != nil {
			t.Fatal(err)
		}
		if k1.D.Cmp(other.D) == 0 {
			t.Error("distinct secrets produced the same key")
		}
	})
}
