//go:build windows
// +build windows

package system

import (
	"os"

	chshare "github.com/proximile/proxiport/share"
)

func ValidateScriptDirOS(fileInfo os.FileInfo, scriptDir string) error {
	return nil
}

func GetScriptExtensionOS(interpreter Interpreter) string {
	isPowershell := interpreter.Matches(chshare.PowerShell, false)

	if isPowershell {
		return PowerShellScriptExt
	}

	return ".bat"
}
