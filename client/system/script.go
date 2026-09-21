package system

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding"

	"github.com/proximile/proxiport/share/random"
)

const DefaultFileMode = os.FileMode(0540)
const DefaultDirMode = os.FileMode(0700)

// PowerShellScriptExt is the extension the Windows agent gives a PowerShell
// script. It is declared here, not in script_win.go, because encodeScript --
// which every platform compiles -- keys the UTF-8 BOM off it.
const PowerShellScriptExt = ".ps1"

// utf8BOM is the byte-order mark Windows PowerShell 5.1 looks for when it
// decides how to decode a script file.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func CreateScriptFile(scriptDir, scriptContent string, interpreter Interpreter, enc *encoding.Encoder) (filePath string, err error) {
	err = ValidateScriptDir(scriptDir)
	if err != nil {
		return "", err
	}

	scriptFileName, err := createScriptFileName(interpreter)
	if err != nil {
		return "", err
	}

	scriptFilePath := filepath.Join(scriptDir, scriptFileName)

	byteContent, err := encodeScript(scriptContent, filepath.Ext(scriptFileName), enc)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(scriptFilePath, byteContent, DefaultFileMode)
	if err != nil {
		return "", err
	}

	return scriptFilePath, nil
}

func ValidateScriptDir(scriptDir string) error {
	if strings.TrimSpace(scriptDir) == "" {
		return errors.New("script directory cannot be empty")
	}

	dirStat, err := os.Stat(scriptDir)

	if os.IsNotExist(err) {
		return fmt.Errorf("script directory %s does not exist", scriptDir)
	}
	if err != nil {
		return err
	}
	if !dirStat.IsDir() {
		return fmt.Errorf("script directory %s is not a valid directory", scriptDir)
	}

	err = ValidateScriptDirOS(dirStat, scriptDir)
	if err != nil {
		return err
	}

	return nil
}

func createScriptFileName(interpreter Interpreter) (string, error) {
	scriptName, err := random.UUID4()
	if err != nil {
		return "", err
	}

	return scriptName + GetScriptExtensionOS(interpreter), nil
}

// encodeScript turns the script body the server sent into the bytes that go on
// disk.
//
// A .ps1 is written as UTF-8 with a BOM and is never transcoded. Without a BOM,
// powershell.exe decodes a script using the host's ANSI code page, so the bytes
// that execute are not the bytes the server audited on any host whose code page
// is not the one the agent guessed -- and a mis-decoded UTF-8 continuation byte
// can land on a character PowerShell accepts as a string delimiter, aborting the
// parse with an error that points nowhere near the cause. The BOM makes the file
// self-describing, so it decodes identically whatever the console code page is,
// including the pages golang.org/x/text has no encoder for. Transcoding to the
// console encoding would also be lossy for anything outside that page; UTF-8
// carries every character the operator wrote.
//
// Everything else keeps the historical behavior: transcode to the detected
// console encoding, and write the raw bytes when detection found none. A .bat
// deliberately does not get a BOM -- cmd.exe does not recognize one and would
// try to execute it.
func encodeScript(scriptContent, ext string, enc *encoding.Encoder) ([]byte, error) {
	raw := []byte(scriptContent)

	if ext == PowerShellScriptExt {
		if bytes.HasPrefix(raw, utf8BOM) {
			return raw, nil
		}
		return append(append([]byte{}, utf8BOM...), raw...), nil
	}

	if enc == nil {
		return raw, nil
	}

	return enc.Bytes(raw)
}

const shebangPrefix = "#!"

// HasShebangLine is just for making code more readable
func HasShebangLine(script string) bool {
	return strings.HasPrefix(script, shebangPrefix)
}
