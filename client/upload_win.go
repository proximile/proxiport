//go:build windows
// +build windows

package chclient

// FileReceptionGlobs is the default protected-destination list for a Windows
// agent. See the Unix list for why this exists and what a pattern means; the
// reasoning is identical, and matching here is case-insensitive.
//
// A pattern ending in "/**" protects that directory and everything under it.
var FileReceptionGlobs = []string{
	`C:\Windows/**`,
	`C:\ProgramData/**`,
	`C:\Program Files/**`,
	`C:\Program Files (x86)/**`,

	// Startup folders: anything dropped here runs at the next logon.
	`C:\Users\*\AppData\Roaming\Microsoft\Windows\Start Menu\Programs\Startup/**`,

	// The agent's own configuration and credential.
	`C:\Program Files\proxiport/**`,
}
