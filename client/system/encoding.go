package system

import (
	"fmt"
	"regexp"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/ianaindex"

	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/clientconfig"
)

var (
	codePageRegexp = regexp.MustCompile(`(\d+)`)
	// Mapping from a Windows code-page identifier to the name
	// golang.org/x/text/encoding/ianaindex answers to.
	//
	// ianaindex resolves a bare number only for the OEM/DOS pages (437, 850,
	// 852, 855, 860, 862, 863, 865, 866); every Windows ANSI page and every
	// DBCS page is rejected as an invalid encoding name. Falling through to
	// the bare number therefore failed detection on every host whose ANSI code
	// page is not 1252 -- which is every non-Western Windows install -- and the
	// caller logs that failure and carries on with no encoder at all.
	//
	// Every name below was checked against the pinned x/text: it resolves and
	// returns a non-nil Encoding. TestCodePageMappingResolves keeps it that
	// way. Pages x/text cannot serve (857, 932's windows-31j alias, TIS-620)
	// are deliberately absent -- they surface as "not supported", which is an
	// honest answer, and .ps1 scripts are written with a BOM so PowerShell
	// never has to guess (see encodeScript).
	codePageToIANAMapping = map[string]string{
		// UTF: 65000 resolves but x/text declines to implement UTF-7, and
		// 65001 is the no-transcoding sentinel detectEncodingByCHCPOutput
		// checks for by name.
		"65000": "utf-7",
		"65001": "utf-8",

		// Windows ANSI pages -- what [System.Text.Encoding]::Default.CodePage
		// returns under Windows PowerShell 5.1.
		"874":  "windows-874",  // Thai
		"932":  "Shift_JIS",    // Japanese
		"936":  "GBK",          // Simplified Chinese
		"949":  "EUC-KR",       // Korean (x/text's EUC-KR decodes CP949/UHC)
		"950":  "Big5",         // Traditional Chinese
		"1250": "windows-1250", // Central European
		"1251": "windows-1251", // Cyrillic
		"1252": "windows-1252", // Western European
		"1253": "windows-1253", // Greek
		"1254": "windows-1254", // Turkish
		"1255": "windows-1255", // Hebrew
		"1256": "windows-1256", // Arabic
		"1257": "windows-1257", // Baltic
		"1258": "windows-1258", // Vietnamese

		// OEM/DOS pages `chcp` can report that the bare number does not reach.
		"858":   "IBM00858", // Western European with the euro sign
		"10000": "macintosh",

		// ISO and KOI8 pages, reachable via `chcp` on a configured host.
		"20127": "US-ASCII",
		"20866": "KOI8-R",
		"21866": "KOI8-U",
		"28591": "ISO-8859-1",
		"28592": "ISO-8859-2",
		"28593": "ISO-8859-3",
		"28594": "ISO-8859-4",
		"28595": "ISO-8859-5",
		"28596": "ISO-8859-6",
		"28597": "ISO-8859-7",
		"28598": "ISO-8859-8",
		"28599": "ISO-8859-9",
		"28603": "ISO-8859-13",
		"28605": "ISO-8859-15",
		"54936": "GB18030",
	}

	detectEncodingCmd              = []string{"/c", "chcp"}
	detectEncodingPowershellInput  = []string{"-Command", "[System.Text.Encoding]::Default.CodePage"}
	detectEncodingPowershellOutput = []string{"-Command", "[Console]::OutputEncoding.CodePage"}
)

func detectEncodingByCHCPOutput(chcpOut string) (encoding.Encoding, error) {
	m := codePageRegexp.FindStringSubmatch(chcpOut)
	if len(m) < 2 {
		return nil, fmt.Errorf("could not parse 'chcp' command output: could not find Code Page number in: %q", chcpOut)
	}

	codePage := m[1]
	iana := getIANAByCodePage(codePage)

	// utf-8 is used by default, no need to return encoding
	if iana == "utf-8" {
		return nil, nil
	}

	enc, err := ianaindex.IANA.Encoding(iana)
	if err != nil {
		return nil, fmt.Errorf("could not get Encoding by IANA name using detected Code Page %s: %v", codePage, err)
	}

	if enc == nil {
		return nil, fmt.Errorf("encoding with Code Page %s is not supported", codePage)
	}

	return enc, nil
}

func getIANAByCodePage(codePage string) string {
	if v, ok := codePageToIANAMapping[codePage]; ok {
		return v
	}

	return codePage
}

func detectEncodingCommand(interpreter Interpreter) ([]string, []string) {
	switch {
	case interpreter.Matches(chshare.CmdShell, false):
		return detectEncodingCmd, nil // nil output encoding implies it's the same as input
	case interpreter.Matches(chshare.PowerShell, false):
		return detectEncodingPowershellInput, detectEncodingPowershellOutput
	default:
		return nil, nil
	}
}

type ShellEncoding struct {
	InputEncoding  encoding.Encoding
	OutputEncoding encoding.Encoding
}

func EncodingFromConfig(config clientconfig.InterpreterAliasEncoding) (*ShellEncoding, error) {
	inputEncoding, err := ianaindex.IANA.Encoding(config.InputEncoding)
	if err != nil {
		return nil, fmt.Errorf("invalid input encoding %q: %w", config.InputEncoding, err)
	}
	outputEncoding, err := ianaindex.IANA.Encoding(config.OutputEncoding)
	if err != nil {
		return nil, fmt.Errorf("invalid output encoding %q: %w", config.OutputEncoding, err)
	}
	return &ShellEncoding{
		InputEncoding:  inputEncoding,
		OutputEncoding: outputEncoding,
	}, nil
}

func (e *ShellEncoding) GetInputEncoder() *encoding.Encoder {
	if e == nil {
		return nil
	}
	if e.InputEncoding == nil {
		return nil
	}
	return e.InputEncoding.NewEncoder()
}

func (e *ShellEncoding) GetOutputDecoder() *encoding.Decoder {
	if e == nil {
		return nil
	}
	if e.OutputEncoding == nil {
		return nil
	}
	return e.OutputEncoding.NewDecoder()
}

func (e *ShellEncoding) String() string {
	if e == nil {
		return "utf-8"
	}
	inputEncodingString := "utf-8"
	if e.InputEncoding != nil {
		inputEncodingString = fmt.Sprint(e.InputEncoding)
	}
	outputEncodingString := "utf-8"
	if e.OutputEncoding != nil {
		outputEncodingString = fmt.Sprint(e.OutputEncoding)
	}
	if inputEncodingString == outputEncodingString {
		return inputEncodingString
	}
	return fmt.Sprintf("input: %s, output: %s", inputEncodingString, outputEncodingString)
}
