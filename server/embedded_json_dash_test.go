package chserver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An embedded field tagged `json:"-"` is the shape that took the fleet down in
// v0.8.3.
//
// Embedding promotes the field's methods onto the outer type, so the call site
// reads t.Terminate(...) with nothing to suggest an interface is involved. The
// json:"-" tag means the field is nil on any value that came back from storage
// or off the wire. Put together: an ordinary-looking method call panics, and it
// panics on exactly the values that survive a restart, which is the state of
// the whole fleet the moment the daemon comes back.
//
// The fix for such a field is a shadowing method on the outer pointer type for
// every method of the embedded type, each deciding what the answer is when
// there is nothing behind it, plus a test that calls them all on a restored
// value. server/clients/clienttunnel does both:
// Tunnel.Start/Terminate/LastActive/SetACL and
// TestTunnelPromotedMethodsAreNilSafe.
//
// This test finds the fields. It cannot tell whether one is guarded, so the
// list is a decision record: a new entry means someone must do the work above
// and then add it here.
var embeddedJSONDashFields = []string{
	"server/clients/clienttunnel/tunnel.go: Tunnel embeds TunnelProtocol",
}

func TestEmbeddedJSONDashFieldsAreKnown(t *testing.T) {
	root := moduleRoot(t)

	var found []string
	skip := map[string]bool{".git": true, "frontend": true, "node_modules": true, "site": true, "testdata": true}

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		fset := token.NewFileSet()
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			// A file this package cannot parse is not one the compiler builds
			// either; leave it to the compiler to complain.
			return nil //nolint:nilerr // parse failures are the build's problem, not this test's
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}

		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			structType, ok := spec.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structType.Fields.List {
				if len(field.Names) != 0 || field.Tag == nil {
					continue
				}
				tag := strings.Trim(field.Tag.Value, "`")
				if reflect.StructTag(tag).Get("json") != "-" {
					continue
				}
				found = append(found,
					filepath.ToSlash(rel)+": "+spec.Name.Name+" embeds "+embeddedTypeName(field.Type))
			}
			return true
		})
		return nil
	})
	require.NoError(t, err)

	sort.Strings(found)
	want := append([]string(nil), embeddedJSONDashFields...)
	sort.Strings(want)

	assert.Equal(t, want, found,
		"the set of embedded `json:\"-\"` fields changed.\n"+
			"Such a field is nil on every value restored from storage or decoded from the "+
			"wire, and embedding promotes its methods, so an ordinary-looking call panics "+
			"on exactly those values. For a new entry: define a shadowing method on the "+
			"outer pointer type for every method of the embedded type, decide what each "+
			"returns when there is nothing behind it, add a test that calls them all on a "+
			"restored value, then list it here. See server/clients/clienttunnel/tunnel.go.")
}

func embeddedTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.StarExpr:
		return "*" + embeddedTypeName(typed.X)
	case *ast.SelectorExpr:
		return embeddedTypeName(typed.X) + "." + typed.Sel.Name
	default:
		return "?"
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "walked past the filesystem root without finding go.mod")
		dir = parent
	}
}
