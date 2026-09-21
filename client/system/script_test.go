package system

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/charmap"
)

func TestValidateScriptDir(t *testing.T) {
	testCases := []struct {
		name             string
		dirToGive        string
		dirModeToGive    os.FileMode
		shouldCreateDir  bool
		shouldCreateFile bool
		errToExpect      string
	}{
		{
			name:            "directory not exists",
			dirToGive:       "non_existing_dir",
			shouldCreateDir: false,
			errToExpect:     "script directory non_existing_dir does not exist",
		},
		{
			name:            "empty dir",
			shouldCreateDir: false,
			errToExpect:     "script directory cannot be empty",
		},
		{
			name:            "empty dir with spaces",
			dirToGive:       "     ",
			shouldCreateDir: false,
			errToExpect:     "script directory cannot be empty",
		},
		{
			name:            "working_dir",
			dirToGive:       "working_dir",
			dirModeToGive:   DefaultDirMode,
			shouldCreateDir: true,
		},
		{
			name:            "working dir with spaces",
			dirToGive:       "  working_dir_space",
			dirModeToGive:   DefaultDirMode,
			shouldCreateDir: true,
		},
		{
			name:             "file as dir name",
			dirToGive:        "some_file",
			shouldCreateFile: true,
			errToExpect:      "script directory some_file is not a valid directory",
		},
	}

	for _, testCase := range testCases {
		tc := testCase
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.shouldCreateFile {
				emptyFile, err := os.Create(tc.dirToGive)
				require.NoError(t, err)

				err = emptyFile.Close()
				require.NoError(t, err)
			}

			if testCase.shouldCreateDir {
				err := os.MkdirAll(tc.dirToGive, tc.dirModeToGive)
				require.NoError(t, err)
			}

			err := ValidateScriptDir(tc.dirToGive)
			if tc.errToExpect != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errToExpect)
			} else {
				require.NoError(t, err)
			}
		})
	}

	for _, testCase := range testCases {
		if !testCase.shouldCreateDir && !testCase.shouldCreateFile {
			continue
		}

		err := os.Remove(testCase.dirToGive)
		if err != nil {
			fmt.Println(err)
		}
	}
}

// TestEncodeScriptWritesPowerShellAsSelfDescribingUTF8 is the general fix for
// L7. The code-page table now covers every Windows ANSI page x/text can serve,
// but a table is a list and a list has an end: 857 (Turkish OEM) has no encoder
// at all, and a page added to Windows tomorrow would not be in it. A .ps1 that
// carries a BOM does not depend on the table being complete, because
// powershell.exe stops guessing.
func TestEncodeScriptWritesPowerShellAsSelfDescribingUTF8(t *testing.T) {
	const bom = "\xEF\xBB\xBF"
	// A script whose non-ASCII bytes are exactly the kind that break a parse
	// when they are decoded as an ANSI page: the second byte of the UTF-8 pair
	// for U+201C is 0x80, and 0x93/0x94 are the smart quotes Windows-1252 puts
	// where PowerShell expects nothing of the sort.
	script := "Write-Output “hello” # éà中文\n"

	t.Run("a .ps1 gets a BOM and its bytes are left alone", func(t *testing.T) {
		got, err := encodeScript(script, PowerShellScriptExt, nil)
		require.NoError(t, err)
		assert.Equal(t, bom+script, string(got))
	})

	t.Run("a .ps1 is not transcoded even when an encoder was detected", func(t *testing.T) {
		// charmap.Windows1252 cannot represent the CJK characters at all, so a
		// transcode here would either fail or silently substitute them.
		got, err := encodeScript(script, PowerShellScriptExt, charmap.Windows1252.NewEncoder())
		require.NoError(t, err)
		assert.Equal(t, bom+script, string(got),
			"the .ps1 must be written as UTF-8 regardless of the console code page")
	})

	t.Run("a BOM already present is not doubled", func(t *testing.T) {
		got, err := encodeScript(bom+script, PowerShellScriptExt, nil)
		require.NoError(t, err)
		assert.Equal(t, bom+script, string(got))
	})

	t.Run("a .bat is still transcoded and gets no BOM", func(t *testing.T) {
		got, err := encodeScript("echo é\r\n", ".bat", charmap.Windows1252.NewEncoder())
		require.NoError(t, err)
		assert.Equal(t, "echo \xe9\r\n", string(got))
		assert.NotContains(t, string(got), bom, "cmd.exe does not recognize a BOM and would try to run it")
	})

	t.Run("a unix script with no detected encoding is written verbatim", func(t *testing.T) {
		got, err := encodeScript("#!/bin/sh\necho é\n", "", nil)
		require.NoError(t, err)
		assert.Equal(t, "#!/bin/sh\necho é\n", string(got))
	})
}
