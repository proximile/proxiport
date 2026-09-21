package fs

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The QueryDosDeviceW call is the one line in this package that no test can
// execute on a non-Windows builder, and it is exactly the line that was wrong:
// it handed the kernel a []byte and a length counted in bytes for an argument
// the API defines as a count of UTF-16 code units. The two tests below are
// source assertions rather than behavioral ones, because the property they
// protect -- "the number handed to the kernel is len() of the buffer the kernel
// is given, in the kernel's own unit" -- is a property of how the call is
// written, and a Linux builder can read that.

func parsePackageFiles(t *testing.T) (*token.FileSet, map[string]*ast.File) {
	t.Helper()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// parser.ParseFile ignores build constraints, so the windows-only and
		// darwin-only files are read here even on a Linux builder.
		f, err := parser.ParseFile(fset, filepath.Join(".", name), nil, parser.ParseComments)
		require.NoError(t, err, "parsing %s", name)
		files[name] = f
	}
	require.Contains(t, files, "fs_windows.go")
	return fset, files
}

func render(fset *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, node); err != nil {
		return "<unprintable>"
	}
	return buf.String()
}

// TestQueryDosDeviceIsToldACodeUnitCount pins the ucchMax contract at the call
// site. It fails against the unfixed tree, where the buffer was
// `make([]byte, 256)` reached through unsafe.Pointer and the length passed was
// that byte count -- which authorized the kernel to write 512 bytes into a
// 256-byte allocation.
func TestQueryDosDeviceIsToldACodeUnitCount(t *testing.T) {
	fset, files := parsePackageFiles(t)

	bufferArg := regexp.MustCompile(`^&([A-Za-z_][A-Za-z0-9_]*)\[0\]$`)
	lengthArg := regexp.MustCompile(`^uint32\(len\(([A-Za-z_][A-Za-z0-9_]*)\)\)$`)

	found := 0
	for name, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "QueryDosDevice" {
				return true
			}
			found++

			require.Len(t, call.Args, 3, "%s: QueryDosDevice takes deviceName, targetPath, ucchMax", name)

			gotBuffer := render(fset, call.Args[1])
			gotLength := render(fset, call.Args[2])

			bufferMatch := bufferArg.FindStringSubmatch(gotBuffer)
			require.NotNil(t, bufferMatch,
				"%s: the target buffer must be passed as &<slice>[0] with no unsafe conversion, got %q", name, gotBuffer)

			lengthMatch := lengthArg.FindStringSubmatch(gotLength)
			require.NotNil(t, lengthMatch,
				"%s: ucchMax must be uint32(len(<slice>)), got %q", name, gotLength)

			assert.Equal(t, bufferMatch[1], lengthMatch[1],
				"%s: ucchMax must be the length of the very buffer being passed", name)

			assertDeclaredAsUint16Slice(t, fset, file, name, bufferMatch[1])
			return true
		})
	}

	require.Equal(t, 1, found, "expected exactly one QueryDosDevice call in this package")
}

// assertDeclaredAsUint16Slice is the half of the invariant the length
// expression cannot carry: uint32(len(buf)) is only a code-unit count if buf is
// a []uint16. A []byte with the same expression is the original defect.
func assertDeclaredAsUint16Slice(t *testing.T, fset *token.FileSet, file *ast.File, fileName, ident string) {
	t.Helper()

	declared := false
	ast.Inspect(file, func(n ast.Node) bool {
		field, ok := n.(*ast.Field)
		if !ok {
			return true
		}
		for _, name := range field.Names {
			if name.Name != ident {
				continue
			}
			assert.Equal(t, "[]uint16", render(fset, field.Type),
				"%s: %s must be a []uint16 -- QueryDosDeviceW counts UTF-16 code units, not bytes", fileName, ident)
			declared = true
		}
		return true
	})

	assert.True(t, declared, "%s: could not find the declaration of %s", fileName, ident)
}

// TestPackageDoesNotUseUnsafe is the general form of the same rule. Passing a
// []byte where the Win32 binding wants a *uint16 is only expressible through
// unsafe; with the buffer typed correctly the package has no use for it, so
// banning the import bans the whole class rather than the one call site.
func TestPackageDoesNotUseUnsafe(t *testing.T) {
	_, files := parsePackageFiles(t)

	for name, file := range files {
		for _, imp := range file.Imports {
			assert.NotEqual(t, `"unsafe"`, imp.Path.Value,
				"%s imports unsafe; size the buffer in the unit the API counts in instead", name)
		}
	}
}
