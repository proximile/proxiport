//go:build !windows
// +build !windows

package chclient

// FileReceptionGlobs is the default protected-destination list for a Unix
// agent. It is the value of [file-reception] protected unless the operator sets
// their own, and it is enforced on the agent, which is the side that matters:
// the server may be hostile, and it is the agent's filesystem at stake.
//
// The list previously held only executable search paths and pseudo-filesystems,
// which left every route to code execution open. A file push carries a mode and
// an owner and the agent will chown as root, so an operator holding only the
// uploads permission -- no commands, no scripts -- could write an
// authorized_keys, a cron file, a sudoers drop-in or a systemd unit, and get
// code execution on the managed host. That is a way around the command and
// script permissions, not a use of them.
//
// A pattern ending in "/**" protects that directory and everything under it;
// anything else is a filepath.Match glob, which does not cross a separator.
var FileReceptionGlobs = []string{
	// Executable search paths and pseudo-filesystems.
	"/bin", "/sbin", "/boot", "/usr/bin", "/usr/sbin",
	"/usr/local/bin", "/usr/local/sbin",
	"/dev", "/lib*", "/usr/lib*", "/run", "/proc", "/sys",

	// Anything that runs on a schedule, as root.
	"/etc/crontab", "/etc/cron.allow", "/etc/cron.deny",
	"/etc/cron.d/**", "/etc/cron.hourly/**", "/etc/cron.daily/**",
	"/etc/cron.weekly/**", "/etc/cron.monthly/**",
	"/var/spool/cron/**",

	// Anything that runs at boot, or defines a service.
	"/etc/systemd/**", "/usr/lib/systemd/**", "/lib/systemd/**",
	"/usr/local/lib/systemd/**",
	"/etc/init.d/**", "/etc/init/**", "/etc/rc.local", "/etc/rc*.d/**",

	// Anything sourced into a root shell, or loaded into every process.
	"/etc/profile", "/etc/profile.d/**", "/etc/bash.bashrc", "/etc/bashrc",
	"/etc/environment", "/etc/ld.so.conf", "/etc/ld.so.conf.d/**",
	"/etc/ld.so.preload",

	// Accounts, authentication and authorization.
	"/etc/passwd", "/etc/shadow", "/etc/group", "/etc/gshadow",
	"/etc/sudoers", "/etc/sudoers.d/**",
	"/etc/pam.d/**", "/etc/security/**",
	"/etc/ssh/**",

	// SSH keys, including the ones that grant a login.
	"/root/**", "/home/*/.ssh/**", "/home/*/.config/systemd/**",
	"/Users/*/.ssh/**",

	// Package-manager hooks, which run as root on the next update.
	"/etc/apt/apt.conf.d/**", "/etc/yum.repos.d/**", "/etc/dnf/**",
	"/etc/apk/**", "/etc/pacman.d/hooks/**",

	// Network and login hooks that execute scripts.
	"/etc/network/if-up.d/**", "/etc/network/if-pre-up.d/**",
	"/etc/NetworkManager/dispatcher.d/**", "/etc/update-motd.d/**",
	"/etc/dhcp/dhclient-exit-hooks.d/**",

	// The agent's own configuration and credential. A push here would
	// repoint the agent at another server.
	"/etc/proxiport/**",
}
