// Package jsonsurface enumerates the JSON keys a Go type will encode.
//
// It exists to make the published shape of a boundary type a thing a test can
// assert on. The project has twice shipped a credential because a struct that
// crosses a trust boundary publishes new fields by default: someone adds a
// field for a local purpose, the struct is already marshaled onto the wire or
// into an API response, and the field goes with it. Nothing at the call site
// says so.
//
// A frozen surface inverts that. A new field fails a test that names the
// boundary it would cross, and the author has to say json:"-" or add the key to
// the golden list on purpose.
package jsonsurface

import (
	"encoding"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
)

var (
	jsonMarshaler = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
	textMarshaler = reflect.TypeOf((*encoding.TextMarshaler)(nil)).Elem()
)

// Of returns every JSON key path the type encodes, sorted and dotted:
// "client.tags", "monitoring.enabled". A map's values contribute a "*"
// segment, since their keys are data rather than schema.
//
// Fields tagged json:"-" are absent, which is the whole point — the returned
// list is what actually leaves the process.
func Of(t reflect.Type) []string {
	keys := map[string]bool{}
	walk(t, "", keys, map[reflect.Type]bool{})

	out := make([]string, 0, len(keys))
	for k := range keys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func walk(t reflect.Type, prefix string, keys map[string]bool, seen map[reflect.Type]bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	// A type that marshals itself is a leaf: whatever it emits is its own
	// business and not a struct surface we can enumerate.
	if prefix != "" && marshalsItself(t) {
		keys[prefix] = true
		return
	}

	switch t.Kind() {
	case reflect.Struct:
		if seen[t] {
			return
		}
		seen[t] = true
		defer delete(seen, t)

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			name, skip, inline := fieldName(field)
			if skip {
				continue
			}
			child := prefix
			if !inline {
				child = join(prefix, name)
			}
			walk(field.Type, child, keys, seen)
		}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			// []byte encodes as a base64 string, not as elements.
			keys[prefix] = true
			return
		}
		walk(t.Elem(), prefix, keys, seen)
	case reflect.Map:
		walk(t.Elem(), join(prefix, "*"), keys, seen)
	case reflect.Interface:
		// The concrete type is not knowable here; record the key itself.
		keys[prefix] = true
	default:
		if prefix != "" {
			keys[prefix] = true
		}
	}
}

func marshalsItself(t reflect.Type) bool {
	if t.Implements(jsonMarshaler) || reflect.PointerTo(t).Implements(jsonMarshaler) {
		return true
	}
	return t.Implements(textMarshaler) || reflect.PointerTo(t).Implements(textMarshaler)
}

// fieldName resolves what encoding/json will call a field, reporting whether it
// is skipped entirely and whether it is an embedded struct whose fields are
// promoted into the parent rather than nested under a key of their own.
func fieldName(field reflect.StructField) (name string, skip bool, inline bool) {
	if !field.IsExported() && !field.Anonymous {
		return "", true, false
	}

	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", true, false
	}

	tagName := strings.Split(tag, ",")[0]
	if tagName != "" {
		return tagName, false, false
	}

	if field.Anonymous {
		t := field.Type
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		// An embedded struct or interface with no tag has its fields promoted.
		if t.Kind() == reflect.Struct || t.Kind() == reflect.Interface {
			return "", false, true
		}
	}
	if !field.IsExported() {
		return "", true, false
	}
	return field.Name, false, false
}

func join(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

// SecretLooking returns the subset of keys whose final segment names something
// that is usually a credential. It is a name check, not a taint analysis: it
// catches the ordinary case where a field called "password" is published,
// which is how this class of bug has actually occurred here.
func SecretLooking(keys []string, allow map[string]bool) []string {
	var hits []string
	for _, key := range keys {
		if allow[key] {
			continue
		}
		segment := key
		if i := strings.LastIndex(key, "."); i >= 0 {
			segment = key[i+1:]
		}
		if looksSecret(segment) {
			hits = append(hits, key)
		}
	}
	return hits
}

// secretWords are matched as substrings of a key's last segment.
var secretWords = []string{
	"auth", "credential", "passphrase", "password", "passwd", "privatekey",
	"private_key", "secret", "seed", "token",
}

func looksSecret(segment string) bool {
	lower := strings.ToLower(segment)
	for _, word := range secretWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}
