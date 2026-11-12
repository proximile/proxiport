package chserver

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"go.yaml.in/yaml/v3"

	"github.com/proximile/proxiport/server/chconfig"
)

// openAPIMethods are the keys of a path item that hold an operation.
var openAPIMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true, "patch": true, "head": true, "options": true,
}

// readOpenAPIFile parses one file of the spec into a mapping.
func readOpenAPIFile(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // path is under a fixed in-repo spec dir, not user input
	if err != nil {
		t.Fatalf("read %s: %v", relPath(path), err)
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not valid YAML: %v", relPath(path), err)
	}
	return doc
}

// TestOpenAPIFilesAreYAMLMappings parses every file of the spec. The spec was
// once stored with every level at the same indentation: most files failed to
// parse and the rest parsed into one flat mapping, so no renderer, linter or
// client generator could load it.
func TestOpenAPIFilesAreYAMLMappings(t *testing.T) {
	files := collectYAMLFiles(t)
	if len(files) == 0 {
		t.Fatalf("no OpenAPI YAML files found under %s", openAPIRoot)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f) //nolint:gosec // f comes from walking a fixed in-repo spec dir, not user input
		if err != nil {
			t.Fatalf("read %s: %v", relPath(f), err)
		}
		var doc map[string]interface{}
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			t.Errorf("%s is not valid YAML: %v", relPath(f), err)
			continue
		}
		if len(doc) == 0 {
			t.Errorf("%s is empty or not a mapping", relPath(f))
		}
	}
}

// openAPIPathItems returns the path items of the spec, keyed by path.
func openAPIPathItems(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	root := readOpenAPIFile(t, filepath.Join(openAPIRoot, "openapi.yaml"))
	paths, ok := root["paths"].(map[string]interface{})
	if !ok || len(paths) == 0 {
		t.Fatalf("openapi.yaml has no paths mapping")
	}
	items := map[string]map[string]interface{}{}
	for path, v := range paths {
		entry, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("path %s is not a mapping", path)
		}
		ref, ok := entry["$ref"].(string)
		if !ok || len(entry) != 1 {
			t.Fatalf("path %s must be a single $ref to a file under paths/", path)
		}
		items[path] = readOpenAPIFile(t, filepath.Join(openAPIRoot, ref))
	}
	return items
}

// TestOpenAPIPathItemsAreWellFormed checks the nesting the rest of the spec
// hangs from: a path item holds operations, an operation holds responses keyed
// by status code, and a response has a description. Flattened or mis-indented
// files fail here even when they still parse.
func TestOpenAPIPathItemsAreWellFormed(t *testing.T) {
	statusRe := regexp.MustCompile(`^([1-5][0-9][0-9]|default)$`)
	for path, item := range openAPIPathItems(t) {
		operations := 0
		for key, v := range item {
			if !openAPIMethods[key] {
				if key != "parameters" && key != "summary" && key != "description" {
					t.Errorf("%s: unexpected key %q in a path item", path, key)
				}
				continue
			}
			operations++
			op, ok := v.(map[string]interface{})
			if !ok {
				t.Errorf("%s %s: the operation is not a mapping", key, path)
				continue
			}
			if id, _ := op["operationId"].(string); id == "" {
				t.Errorf("%s %s: no operationId", key, path)
			}
			responses, ok := op["responses"].(map[string]interface{})
			if !ok || len(responses) == 0 {
				t.Errorf("%s %s: no responses", key, path)
				continue
			}
			for status, r := range responses {
				if !statusRe.MatchString(status) {
					t.Errorf("%s %s: response key %q is not a status code", key, path, status)
				}
				resp, ok := r.(map[string]interface{})
				if !ok {
					t.Errorf("%s %s: response %s is not a mapping", key, path, status)
					continue
				}
				if d, _ := resp["description"].(string); d == "" {
					t.Errorf("%s %s: response %s has no description", key, path, status)
				}
			}
		}
		if operations == 0 {
			t.Errorf("%s: the path item has no operation", path)
		}
	}
}

// undocumentedRoutes are routes the spec leaves out on purpose.
var undocumentedRoutes = map[string]bool{
	// Stubs that answer 410 Gone for a removed endpoint.
	"PUT /me/token/{}":    true,
	"DELETE /me/token/{}": true,
}

// TestOpenAPIOperationsMatchRouter compares the operations of the spec with the
// routes the API router serves, so that neither a documented endpoint that does
// not exist nor a served endpoint that is not documented can slip in. Parameter
// names are not compared, only where the parameters are.
func TestOpenAPIOperationsMatchRouter(t *testing.T) {
	paramRe := regexp.MustCompile(`\{[^}]*\}`)
	key := func(method, path string) string {
		return strings.ToUpper(method) + " " + paramRe.ReplaceAllString(path, "{}")
	}

	documented := map[string]bool{}
	for path, item := range openAPIPathItems(t) {
		for method := range item {
			if openAPIMethods[method] {
				documented[key(method, path)] = true
			}
		}
	}

	al := &APIListener{Logger: testLog, insecureForTests: true, Server: &Server{config: &chconfig.Config{}}}
	al.initRouter()
	served := map[string]bool{}
	err := al.router.Walk(func(route *mux.Route, _ *mux.Router, _ []*mux.Route) error {
		tpl, err := route.GetPathTemplate()
		if err != nil || !strings.HasPrefix(tpl, "/api/v1/") {
			return nil
		}
		methods, err := route.GetMethods()
		if err != nil {
			return nil // a subrouter's prefix, not an endpoint
		}
		for _, m := range methods {
			served[key(m, strings.TrimPrefix(tpl, "/api/v1"))] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk the router: %v", err)
	}
	if len(served) < 50 || len(documented) < 50 {
		t.Fatalf("found %d served and %d documented operations; the comparison would prove nothing", len(served), len(documented))
	}

	var problems []string
	for k := range documented {
		if !served[k] {
			problems = append(problems, "documented but not served: "+k)
		}
	}
	for k := range served {
		if !documented[k] && !undocumentedRoutes[k] {
			problems = append(problems, "served but not documented: "+k)
		}
	}
	for k := range undocumentedRoutes {
		if !served[k] {
			problems = append(problems, "listed as undocumented but not served: "+k)
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}
