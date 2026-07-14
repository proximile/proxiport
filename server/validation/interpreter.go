package validation

import (
	"fmt"

	chshare "github.com/proximile/proxiport/share"
)

var validInputInterpreter = []string{chshare.CmdShell, chshare.PowerShell}

func ValidateInterpreter(interpreter string, isScript bool) error {
	// we skip validation for scripts because server is not able to detect invalid values as user might use
	// interpreter aliases or full paths which are not accessible on the server
	if interpreter == "" || isScript {
		return nil
	}

	for _, v := range validInputInterpreter {
		if interpreter == v {
			return nil
		}
	}

	return fmt.Errorf("expected interpreter to be one of: %s, actual: %s", validInputInterpreter, interpreter)
}
