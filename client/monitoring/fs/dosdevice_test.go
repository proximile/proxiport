package fs

import (
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeQueryDosDevice models QueryDosDeviceW's actual contract: it may store at
// most ucchMax UTF-16 code units, and reports ERROR_INSUFFICIENT_BUFFER when
// the answer does not fit. Because the production signature hands it the slice
// itself, "at most ucchMax" is enforced by the type rather than promised in a
// comment -- which is the whole point of the fix this file covers.
func fakeQueryDosDevice(names []string, bufLens *[]int) dosDeviceQuery {
	return func(buf []uint16) (uint32, error) {
		*bufLens = append(*bufLens, len(buf))

		var out []uint16
		for _, name := range names {
			out = append(out, utf16.Encode([]rune(name))...)
			out = append(out, 0)
		}
		out = append(out, 0) // the list is terminated by a second NUL

		if len(out) > len(buf) {
			return 0, errBufferTooSmall
		}
		copy(buf, out)
		return uint32(len(out)), nil //nolint:gosec // test fixture; out is a few hundred units
	}
}

func TestRemoteDriveFSType(t *testing.T) {
	// A DFS/WebDAV target, which is what makes a drive letter's NT path run
	// past the 127 characters the old 256-byte buffer could actually hold.
	longSMB := `\Device\LanmanRedirector\;Z:0000000000abcdef\fileserver.example.invalid\` +
		strings.Repeat("deeply-nested-share-segment\\", 30) + "final"

	testCases := []struct {
		name       string
		deviceList []string
		wantFSType string
	}{
		{
			name:       "an ordinary local volume is not a network share",
			deviceList: []string{`\Device\HarddiskVolume3`},
			wantFSType: "",
		},
		{
			name:       "an SMB mapping is reported as smbfs",
			deviceList: []string{`\Device\LanmanRedirector\;Z:0000000000abcdef\server\share`},
			wantFSType: "smbfs",
		},
		{
			name:       "an NFS mapping is reported as nfs",
			deviceList: []string{`\Device\MRxNfs\;Z:0000000000abcdef\server\export`},
			wantFSType: "nfs",
		},
		{
			name:       "a target far longer than the old buffer is still recognized",
			deviceList: []string{longSMB},
			wantFSType: "smbfs",
		},
		{
			name:       "a multi-entry list is searched entry by entry",
			deviceList: []string{`\Device\HarddiskVolume3`, `\Device\MRxNfs\;Z:0\server\export`},
			wantFSType: "nfs",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var bufLens []int
			got, err := remoteDriveFSType(fakeQueryDosDevice(tc.deviceList, &bufLens))
			require.NoError(t, err)
			assert.Equal(t, tc.wantFSType, got)

			// Every call must have been given a buffer whose length is what the
			// kernel was told it could fill. The fake asserts this by
			// construction -- it can only write into the slice it is handed --
			// so the meaningful check is that a call happened at all.
			require.NotEmpty(t, bufLens)
			for _, l := range bufLens {
				assert.GreaterOrEqual(t, l, dosDeviceInitialBufLen)
			}
		})
	}
}

func TestQueryDosDeviceNamesGrowsTheBufferRatherThanTruncating(t *testing.T) {
	// One name that does not fit in the initial buffer but does fit in the next.
	name := `\Device\LanmanRedirector\` + strings.Repeat("x", dosDeviceInitialBufLen)

	var bufLens []int
	names, err := queryDosDeviceNames(fakeQueryDosDevice([]string{name}, &bufLens))
	require.NoError(t, err)

	require.Equal(t, []string{name}, names, "the name must come back whole, not truncated")
	assert.Equal(t, []int{dosDeviceInitialBufLen, dosDeviceInitialBufLen * 2}, bufLens,
		"the first attempt should be refused and the second should be twice as large")
}

func TestQueryDosDeviceNamesGivesUpAtTheCap(t *testing.T) {
	var bufLens []int
	tooSmallForever := func(buf []uint16) (uint32, error) {
		bufLens = append(bufLens, len(buf))
		return 0, errBufferTooSmall
	}

	_, err := queryDosDeviceNames(tooSmallForever)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "more than")

	require.NotEmpty(t, bufLens)
	assert.Equal(t, dosDeviceMaxBufLen, bufLens[len(bufLens)-1],
		"the last attempt should be at the cap, and there should be no attempt past it")
}

func TestQueryDosDeviceNamesRefusesAnImpossibleLength(t *testing.T) {
	// A kernel that claims to have stored more than it was given room for. The
	// old code could not notice this; the new code must not read past the write.
	liar := func(buf []uint16) (uint32, error) {
		return uint32(len(buf) + 1), nil //nolint:gosec // test fixture; len(buf) is dosDeviceInitialBufLen
	}

	_, err := queryDosDeviceNames(liar)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UTF-16 code units")
}

func TestSplitDosDeviceNames(t *testing.T) {
	encode := func(units ...interface{}) []uint16 {
		var out []uint16
		for _, u := range units {
			switch v := u.(type) {
			case string:
				out = append(out, utf16.Encode([]rune(v))...)
			case int:
				out = append(out, uint16(v)) //nolint:gosec // test fixture; only 0 is passed
			}
		}
		return out
	}

	testCases := []struct {
		name string
		buf  []uint16
		want []string
	}{
		{name: "empty buffer", buf: nil, want: nil},
		{name: "only terminators", buf: encode(0, 0), want: nil},
		{
			name: "one NUL-terminated name",
			buf:  encode(`\Device\HarddiskVolume1`, 0, 0),
			want: []string{`\Device\HarddiskVolume1`},
		},
		{
			name: "several names",
			buf:  encode(`\Device\A`, 0, `\Device\B`, 0, 0),
			want: []string{`\Device\A`, `\Device\B`},
		},
		{
			name: "an unterminated tail is still returned",
			buf:  encode(`\Device\A`),
			want: []string{`\Device\A`},
		},
		{
			name: "a surrogate pair survives the round trip",
			buf:  encode("\U0001F5B4", 0, 0),
			want: []string{"\U0001F5B4"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, splitDosDeviceNames(tc.buf))
		})
	}
}
