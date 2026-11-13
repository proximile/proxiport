// Package utcprocess pins the process time zone to UTC for a test binary.
//
// Import it for its side effect from the tests of a package that compares
// what it stored in SQLite with what it reads back:
//
//	import _ "github.com/proximile/proxiport/share/test/utcprocess"
//
// A time read back from SQLite is the instant that was stored, but it carries
// time.Local when its offset is the process's own, while a fixture built in
// UTC carries time.UTC. assert.Equal compares the two Location pointers too,
// so the same instant compares unequal, even in a process whose zone is UTC.
// With time.Local set to time.UTC the two are the same pointer.
//
// How times behave in other zones is tested in db/sqlite, which sets a zone
// explicitly.
package utcprocess

import "time"

func init() { time.Local = time.UTC }
