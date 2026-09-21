package system

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/simplifiedchinese"

	chshare "github.com/proximile/proxiport/share"
)

func TestDetectCmdOutputEncoding(t *testing.T) {
	testCases := []struct {
		Name         string
		CmdOutput    string
		WantEncoding encoding.Encoding
		WantErr      error
	}{
		{
			Name:         "Code page 850",
			CmdOutput:    "Aktive Codepage: 850.",
			WantEncoding: charmap.CodePage850,
		},
		{
			Name:      "utf-7",
			CmdOutput: "Active code page: 65000.",
			WantErr:   fmt.Errorf("encoding with Code Page %s is not supported", "65000"),
		},
		{
			Name:      "not supported",
			CmdOutput: "Active code page: 869.",
			WantErr:   fmt.Errorf("encoding with Code Page %s is not supported", "869"),
		},
		{
			Name:         "utf-8",
			CmdOutput:    "Active code page: 65001.",
			WantEncoding: nil,
		},
		{
			Name:         "Code page 437",
			CmdOutput:    "Active code page: 437.",
			WantEncoding: charmap.CodePage437,
		},
		{
			Name:         "Code page 1252",
			CmdOutput:    "Active Codepage: 1252.",
			WantEncoding: charmap.Windows1252,
		},
		{
			// Simplified Chinese. This case used to assert the defect: the bare
			// "936" was handed to ianaindex, which rejects it, so detection
			// failed and the agent wrote the script with no encoder at all.
			Name:         "Code page 936",
			CmdOutput:    "Active code page: 936.",
			WantEncoding: simplifiedchinese.GBK,
		},
		{
			// Turkish OEM. ianaindex knows the name and has no encoder for it,
			// so detection fails honestly -- and the .ps1 is written with a BOM
			// regardless, which is why that failure is survivable.
			Name:      "a code page x/text has no encoder for",
			CmdOutput: "Active code page: 857.",
			WantErr:   fmt.Errorf("encoding with Code Page %s is not supported", "857"),
		},
		{
			Name:      "invalid",
			CmdOutput: "some unknown output",
			WantErr:   fmt.Errorf("could not parse 'chcp' command output: could not find Code Page number in: %q", "some unknown output"),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			gotEnc, gotErr := detectEncodingByCHCPOutput(tc.CmdOutput)
			assert.Equal(t, tc.WantErr, gotErr)
			assert.Equal(t, tc.WantEncoding, gotEnc)
		})
	}
}

func TestDetectEncodingCommand(t *testing.T) {
	testCases := []struct {
		Interpreter string
		WantInput   []string
		WantOutput  []string
	}{
		{
			Interpreter: chshare.CmdShell,
			WantInput:   detectEncodingCmd,
			WantOutput:  nil,
		},
		{
			Interpreter: chshare.PowerShell,
			WantInput:   detectEncodingPowershellInput,
			WantOutput:  detectEncodingPowershellOutput,
		},
		{
			Interpreter: chshare.UnixShell,
			WantInput:   nil,
			WantOutput:  nil,
		},
		{
			Interpreter: `C:\Program Files\PowerShell\7\pwsh.exe`,
			WantInput:   detectEncodingPowershellInput,
			WantOutput:  detectEncodingPowershellOutput,
		},
		{
			Interpreter: `C:\Program Files\Git\bin\bash.exe`,
			WantInput:   nil,
			WantOutput:  nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Interpreter, func(t *testing.T) {
			t.Parallel()

			interpreter := Interpreter{
				InterpreterNameFromInput: tc.Interpreter,
			}

			gotInput, gotOutput := detectEncodingCommand(interpreter)
			assert.Equal(t, tc.WantInput, gotInput)
			assert.Equal(t, tc.WantOutput, gotOutput)
		})
	}
}

// TestEveryWindowsANSICodePageResolves is the general form of L7: whatever the
// system locale of a Windows agent, [System.Text.Encoding]::Default.CodePage
// returns one of these, and every one of them has to produce a usable encoder.
// Before the mapping was filled in, eight of the nine failed -- 1252 was the
// only ANSI page in the table -- and the failure was swallowed into a log line
// while the script was written anyway.
func TestEveryWindowsANSICodePageResolves(t *testing.T) {
	windowsANSICodePages := []struct {
		CodePage string
		Locale   string
	}{
		{"874", "Thai"},
		{"932", "Japanese"},
		{"936", "Simplified Chinese"},
		{"949", "Korean"},
		{"950", "Traditional Chinese"},
		{"1250", "Central European"},
		{"1251", "Cyrillic"},
		{"1252", "Western European"},
		{"1253", "Greek"},
		{"1254", "Turkish"},
		{"1255", "Hebrew"},
		{"1256", "Arabic"},
		{"1257", "Baltic"},
		{"1258", "Vietnamese"},
	}

	for _, tc := range windowsANSICodePages {
		tc := tc
		t.Run(tc.CodePage+" ("+tc.Locale+")", func(t *testing.T) {
			t.Parallel()

			enc, err := detectEncodingByCHCPOutput("Active code page: " + tc.CodePage + ".")
			require.NoError(t, err, "code page %s (%s) must be detectable", tc.CodePage, tc.Locale)
			require.NotNil(t, enc, "code page %s (%s) must yield an encoding", tc.CodePage, tc.Locale)

			// An encoder that cannot round-trip plain ASCII would be worse than
			// none at all, so check the mapping actually points somewhere sane.
			got, err := enc.NewEncoder().String("echo ok")
			require.NoError(t, err)
			assert.Equal(t, "echo ok", got)
		})
	}
}

// TestCodePageMappingResolves keeps the table honest: every name in it must be
// one golang.org/x/text answers to. A future entry that x/text cannot serve
// would otherwise reintroduce L7 silently for that locale.
func TestCodePageMappingResolves(t *testing.T) {
	// utf-8 is the sentinel detectEncodingByCHCPOutput checks for by name and
	// returns early on; utf-7 resolves but x/text declines to implement it, and
	// the "not supported" error that produces is asserted above.
	sentinels := map[string]bool{"65001": true, "65000": true}

	for codePage, ianaName := range codePageToIANAMapping {
		codePage, ianaName := codePage, ianaName
		if sentinels[codePage] {
			continue
		}
		t.Run(codePage, func(t *testing.T) {
			t.Parallel()

			enc, err := ianaindex.IANA.Encoding(ianaName)
			require.NoError(t, err, "code page %s maps to %q, which ianaindex rejects", codePage, ianaName)
			require.NotNil(t, enc, "code page %s maps to %q, which ianaindex has no encoder for", codePage, ianaName)
		})
	}
}
