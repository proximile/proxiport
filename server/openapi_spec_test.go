package chserver

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// openAPIRoot is the split OpenAPI spec, relative to this package directory.
const openAPIRoot = "../api-doc/openapi"

// mediaTypeRe matches a syntactically valid HTTP media type, optionally with
// parameters — e.g. "application/json" or "text/plain; charset=utf-8" — plus
// the OpenAPI wildcards "*/*" and "application/*". A trailing colon or stray
// whitespace, as in the malformed key 'application/json:', does not match.
var mediaTypeRe = regexp.MustCompile(`^(\*|[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]*)/(\*|[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]*)\s*(;.*)?$`)

// quotedKeyRe{Single,Double} extract a quoted mapping key, e.g. the
// `application/json:` from a line like `'application/json:': ` (the trailing
// `:` after the closing quote is the YAML key separator). Two patterns because
// Go's RE2 has no backreferences to match the opening quote.
var (
	quotedKeyReSingle = regexp.MustCompile(`^\s*'([^']*)'\s*:`)
	quotedKeyReDouble = regexp.MustCompile(`^\s*"([^"]*)"\s*:`)
)

// TestOpenAPIMediaTypeKeysAreWellFormed guards a whole class of spec drift: a
// malformed content-type key such as 'application/json:' (note the trailing
// colon inside the quotes). That parses as a live key but breaks strict
// OpenAPI tooling / codegen — it is exactly the bug that shipped in
// me_token.yaml.
//
// The check is textual rather than structural on purpose: the spec is authored
// with a 1-space indentation style that a strict YAML parser rejects wholesale
// (nesting collapses into sibling keys), so a parse-tree walk is not viable
// here. A quoted mapping key containing "/" is treated as a media type and must
// be well formed; that precisely targets the malformed-content-key class with
// no false positives on ordinary schema keys.
func TestOpenAPIMediaTypeKeysAreWellFormed(t *testing.T) {
	files := collectYAMLFiles(t, openAPIRoot)
	if len(files) == 0 {
		t.Fatalf("no OpenAPI YAML files found under %s", openAPIRoot)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // f comes from walking a fixed in-repo spec dir, not user input
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, bad := range malformedMediaKeysOnLine(line) {
				t.Errorf("%s:%d: malformed media-type key %q", relPath(f), i+1, bad)
			}
		}
	}
}

// malformedMediaKeysOnLine returns any quoted mapping key on the line that
// looks like a media type (contains "/") but is not well formed.
func malformedMediaKeysOnLine(line string) []string {
	m := quotedKeyReSingle.FindStringSubmatch(line)
	if m == nil {
		m = quotedKeyReDouble.FindStringSubmatch(line)
	}
	if m == nil {
		return nil
	}
	key := m[1]
	if strings.Contains(key, "/") && !mediaTypeRe.MatchString(key) {
		return []string{key}
	}
	return nil
}

// TestOpenAPIMediaTypeDetector proves the linter actually flags the malformed
// shape it is meant to catch (and accepts valid ones), so it can never silently
// succeed if the detection logic regresses.
func TestOpenAPIMediaTypeDetector(t *testing.T) {
	bad := []string{
		` 'application/json:':`, // the me_token.yaml bug: trailing colon
		` 'application/ json':`, // space where the subtype should start
	}
	for _, line := range bad {
		if got := malformedMediaKeysOnLine(line); len(got) == 0 {
			t.Errorf("detector missed malformed key on %q", line)
		}
	}
	good := []string{
		` application/json:`,   // unquoted, valid (key is application/json)
		` 'application/json':`, // quoted, valid
		` '*/*':`,              // wildcard
		` 'text/plain; charset=utf-8':`,
		` description: has/slash: in prose`, // key is "description", not a media type
		` type: string`,                     // ordinary schema key
	}
	for _, line := range good {
		if got := malformedMediaKeysOnLine(line); len(got) != 0 {
			t.Errorf("detector false-positived on %q: %v", line, got)
		}
	}
}

// TestOpenAPIPathRefsResolve asserts every relative $ref in the spec points at
// a file that exists — catching a documented route or schema that references a
// missing file (another drift class).
func TestOpenAPIPathRefsResolve(t *testing.T) {
	files := collectYAMLFiles(t, openAPIRoot)
	refRe := regexp.MustCompile(`\$ref:\s*([^\s#]+)`)
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // f comes from walking a fixed in-repo spec dir, not user input
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range refRe.FindAllStringSubmatch(string(raw), -1) {
			target := strings.Trim(m[1], `"'`)
			if strings.HasPrefix(target, "#") || strings.HasPrefix(target, "http") {
				continue // in-document or remote ref
			}
			resolved := filepath.Join(filepath.Dir(f), target)
			if _, err := os.Stat(resolved); err != nil { //nolint:gosec // resolved is under a fixed in-repo spec dir
				t.Errorf("%s: $ref %q does not resolve (%s)", relPath(f), target, resolved)
			}
		}
	}
}

func collectYAMLFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && (strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")) {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

func relPath(p string) string {
	if r, err := filepath.Rel(openAPIRoot, p); err == nil {
		return r
	}
	return p
}
