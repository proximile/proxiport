package main

import (
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/proximile/proxiport/server/chconfig"
	chshare "github.com/proximile/proxiport/share"
)

// directFlags act on their own instead of overriding a config setting.
var directFlags = map[string]bool{"config": true, "service": true, "service-user": true}

func definedFlags() map[string]bool {
	defined := map[string]bool{}
	add := func(f *pflag.Flag) { defined[f.Name] = true }
	RootCmd.Flags().VisitAll(add)
	RootCmd.PersistentFlags().VisitAll(add)
	return defined
}

func sorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// A flag has three parts: its definition, its binding to a config setting and
// its entry in the help text. They are written in three places, and each of
// the tests below fails when one of them is missing.

func TestEveryFlagOverridesAConfigSetting(t *testing.T) {
	if err := bindPFlags(); err != nil {
		t.Fatal(err)
	}

	bound := map[string]bool{}
	for _, b := range flagBindings {
		if bound[b.flag] {
			t.Errorf("--%s is bound twice", b.flag)
		}
		bound[b.flag] = true
	}
	for _, name := range sorted(definedFlags()) {
		if !bound[name] && !directFlags[name] {
			t.Errorf("--%s is defined but overrides no config setting", name)
		}
	}
}

func TestHelpListsExactlyTheDefinedFlags(t *testing.T) {
	inHelp := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^ +--([a-z0-9-]+),`).FindAllStringSubmatch(serverHelp, -1) {
		inHelp[m[1]] = true
	}
	// cobra adds these two itself.
	delete(inHelp, "help")
	delete(inHelp, "version")

	defined := definedFlags()
	for _, name := range sorted(defined) {
		if !inHelp[name] {
			t.Errorf("--%s is defined but missing from the help text", name)
		}
	}
	for _, name := range sorted(inHelp) {
		if !defined[name] {
			t.Errorf("the help text describes --%s, which is not defined", name)
		}
	}
}

func TestFlagValuesReachTheConfig(t *testing.T) {
	if err := bindPFlags(); err != nil {
		t.Fatal(err)
	}
	flags := RootCmd.Flags()
	for name, value := range map[string]string{
		"auth":                             "agent:secret",
		"data-dir":                         "/tmp/flags-test",
		"use-ports":                        "1000-1100,1200",
		"check-port-timeout":               "5s",
		"max-request-bytes":                "2048",
		"monitoring-enabled":               "false",
		"monitoring-data-storage-duration": "36h",
	} {
		if err := flags.Set(name, value); err != nil {
			t.Fatalf("set --%s: %v", name, err)
		}
	}

	got := &chconfig.Config{}
	if err := chshare.DecodeViperConfig(viperCfg, got, strings.NewReader("")); err != nil {
		t.Fatal(err)
	}

	if got.Server.Auth != "agent:secret" {
		t.Errorf("--auth: got %q", got.Server.Auth)
	}
	if got.Server.DataDir != "/tmp/flags-test" {
		t.Errorf("--data-dir: got %q", got.Server.DataDir)
	}
	if strings.Join(got.Server.UsedPortsRaw, " ") != "1000-1100 1200" {
		t.Errorf("--use-ports: got %q", got.Server.UsedPortsRaw)
	}
	if got.Server.CheckPortTimeout != 5*time.Second {
		t.Errorf("--check-port-timeout: got %s", got.Server.CheckPortTimeout)
	}
	if got.API.MaxRequestBytes != 2048 {
		t.Errorf("--max-request-bytes: got %d", got.API.MaxRequestBytes)
	}
	if got.Monitoring.Enabled {
		t.Error("--monitoring-enabled=false left monitoring on")
	}
	if got.Monitoring.DataStorageDuration != "36h" {
		t.Errorf("--monitoring-data-storage-duration: got %q", got.Monitoring.DataStorageDuration)
	}

	// A flag that was not given must not displace a default.
	if !got.Server.AuthWrite {
		t.Error("auth_write lost its default of true")
	}
	if got.API.MaxFilePushSize != DefaultMaxFilePushBytes {
		t.Errorf("max_filepush_size lost its default: got %d", got.API.MaxFilePushSize)
	}
	if strings.Join(got.Server.ExcludedPortsRaw, " ") != DefaultExcludedPorts {
		t.Errorf("excluded_ports lost its default: got %q", got.Server.ExcludedPortsRaw)
	}
}
