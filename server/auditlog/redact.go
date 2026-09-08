package auditlog

import (
	"bytes"
	"encoding/json"
	"strings"
)

// redactedPlaceholder marks a field that held a secret. It is written in place
// of the value rather than deleting the key, so the record still shows that a
// credential was supplied — which is often the auditable fact.
const redactedPlaceholder = "[redacted]"

// secretFields are object keys whose values never belong in the audit log.
//
// The audit log stores whole request and response bodies, so it inherits every
// credential any handler happens to pass it. A tunnel's HTTP basic-auth
// password reached it exactly that way: handlePutClientTunnel records the
// models.Remote it was given, and that struct carries auth_password in clear
// text. Redacting by field name here fixes that at the one place every entry
// passes through, so a handler cannot leak a new credential into the log by
// adding a field to a struct it already logs.
//
// Keys are matched case-insensitively against the JSON field name at any depth.
var secretFields = map[string]bool{
	"api_token":        true,
	"auth":             true,
	"auth_pass":        true,
	"auth_password":    true,
	"client_secret":    true,
	"credential":       true,
	"current_password": true,
	"key_seed":         true,
	"new_password":     true,
	"old_password":     true,
	"passphrase":       true,
	"password":         true,
	"private_key":      true,
	"proxy":            true,
	"proxy_url":        true,
	"secret":           true,
	"token":            true,
	"totp_secret":      true,
}

// redactSecrets rewrites marshaled JSON, replacing the value of every field
// named in secretFields at any depth.
//
// Decoding with UseNumber keeps numbers in their original text, so a re-encoded
// body is byte-comparable for everything that was not redacted. Input that does
// not parse is returned unchanged; it comes straight from json.Marshal, so that
// is unreachable in practice, and passing it through keeps the entry honest
// rather than silently emptying it.
func redactSecrets(raw []byte) []byte {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return raw
	}

	redacted, changed := redactValue(decoded)
	if !changed {
		return raw
	}

	out, err := json.Marshal(redacted)
	if err != nil {
		return raw
	}
	return out
}

func redactValue(v any) (any, bool) {
	changed := false
	switch typed := v.(type) {
	case map[string]any:
		for key, value := range typed {
			if secretFields[strings.ToLower(key)] && isSet(value) {
				typed[key] = redactedPlaceholder
				changed = true
				continue
			}
			replaced, childChanged := redactValue(value)
			if childChanged {
				typed[key] = replaced
				changed = true
			}
		}
		return typed, changed
	case []any:
		for i, value := range typed {
			replaced, childChanged := redactValue(value)
			if childChanged {
				typed[i] = replaced
				changed = true
			}
		}
		return typed, changed
	default:
		return v, false
	}
}

// isSet reports whether a field actually held something. An absent or empty
// credential is left alone: writing "[redacted]" over it would claim a secret
// was supplied when none was.
func isSet(v any) bool {
	if v == nil {
		return false
	}
	if s, ok := v.(string); ok {
		return s != ""
	}
	return true
}
