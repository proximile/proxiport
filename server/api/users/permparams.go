package users

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// PermissionParams is a JSON-roundtrippable map persisted on user groups for
// the extended-permissions feature. Enforcement was implemented in the
// closed-source `plus/` package and has been removed. The schema and shape of
// this type are preserved so existing rows and API payloads keep working;
// once an OSS extended-permission engine ships, it will read/validate the
// same map.
type PermissionParams map[string]interface{}

func (m PermissionParams) Value() (driver.Value, error) {
	if len(m) == 0 {
		return nil, nil
	}
	return json.Marshal(m)
}

func (m *PermissionParams) Scan(src interface{}) error {
	var source []byte
	switch v := src.(type) {
	case nil:
		return nil
	case string:
		source = []byte(v)
	case []byte:
		source = v
	default:
		return fmt.Errorf("incompatible type %T for PermissionParams", src)
	}
	parsed := map[string]interface{}{}
	if err := json.Unmarshal(source, &parsed); err != nil {
		return err
	}
	*m = PermissionParams(parsed)
	return nil
}
