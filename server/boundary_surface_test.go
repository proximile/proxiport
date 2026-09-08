package chserver

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/clients"
	"github.com/proximile/proxiport/server/clients/clienttunnel"
	"github.com/proximile/proxiport/server/clientsauth"
	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/clientconfig"
	"github.com/proximile/proxiport/share/test/jsonsurface"
)

// This file freezes the JSON surface of every struct that crosses a trust
// boundary, and asserts that nothing credential-shaped sits on one.
//
// It exists because the same bug shipped twice. Both times a field was added to
// a struct for a local reason, the struct was already being marshaled across a
// boundary, and the field went with it. Nothing at the point of the edit said
// so: ClientConfig is thirty fields of ordinary agent settings, and two of them
// happened to be the agent's password.
//
// The lists below are a decision record, not a snapshot to regenerate. A diff
// here means a field is newly published to, or newly withheld from, the party
// the test names. Read the failure, decide whether that party should see it,
// then either tag the field json:"-" or add the key here deliberately.
//
// The type system cannot express the runtime half of the same guard, so two
// other tests carry it: TestClientPayloadRedactsTunnelPassword (server/clients)
// for a field that must exist in the payload but must never carry a value, and
// TestRedactSecretsCoversNestedBodies (server/auditlog) for the audit log.

// secretKeyAllowList records every credential-shaped key that is on a boundary
// on purpose. Each entry is a claim that someone checked; the reason stays with
// it.
var secretKeyAllowList = map[string]bool{
	// Agent -> server. The agent is telling the server which basic-auth
	// credential to put in front of its own tunnels, and the server has to
	// receive it to enforce it. This is the agent handing over its own secret,
	// not the server publishing someone else's.
	"Remotes.auth_password": true,
	"Remotes.auth_user":     true,

	// Server -> API. The identifier half of an agent credential. Operators
	// already address credentials by it, and it grants nothing without the
	// password.
	"client_auth_id": true,

	// Server -> API. The key stays so the response shape does not change for
	// existing callers, but the value is cleared on the way out by
	// clients.redactTunnels. That value-level guarantee is asserted by
	// TestClientPayloadRedactsTunnelPassword, which a type-level check cannot
	// make.
	"tunnels.auth_password": true,
	"tunnels.auth_user":     true,

	// The tunnel model itself must keep the password: the HTTP proxy reads it to
	// check incoming requests, and an empty one makes that check pass entirely.
	// Redaction belongs at the payload boundary, not here. The same two keys
	// cover TunnelPayload, where convertToTunnelPayload clears the value --
	// asserted by TestConvertToTunnelPayloadRedactsPassword.
	"auth_password": true,
	"auth_user":     true,

	// clientsauth.ClientAuth is the credential store's own type, and it is the
	// POST request body as well as the GET response, so the field has to exist.
	// The read handlers build a separate payload type holding only the id --
	// they must not clear this one, because two providers hand back a pointer
	// into their own state and clearing it destroyed the stored credential.
	// TestGetClientsAuthDoesNotDestroyTheCredential and
	// TestSingleProviderHandsOutCopies hold that line; publishing the value
	// would hand out bcrypt hashes to crack offline.
	"password": true,

	// A client group's match parameters address credentials by id, not by
	// secret. Same reasoning as client_auth_id above.
	"params.client_auth_id": true,

	// The login response's own token, issued to the caller who just
	// authenticated. Returning it is the point of the route.
	"token": true,

	// A boolean, matched by the name check because it contains "password".
	// Whether a user must change their password is not itself a secret.
	"password_expired": true,
}

// assertSurface compares a boundary's published keys against its decision
// record, and reports the difference in both directions.
func assertSurface(t *testing.T, boundary string, typ reflect.Type, want []string) {
	t.Helper()

	got := jsonsurface.Of(typ)
	assert.Equal(t, want, got,
		"the JSON surface of this struct changed, and it crosses a trust boundary: "+
			boundary+". A key added here is published to that party from now on; a key "+
			"removed stops being. Decide which you meant, then either tag the field "+
			`json:"-" or update the list in this file.`)

	if leaks := jsonsurface.SecretLooking(got, secretKeyAllowList); len(leaks) > 0 {
		t.Errorf("credential-shaped keys are published across %s: %v\n"+
			`Tag the field json:"-", or add it to secretKeyAllowList with the reason it is safe.`,
			boundary, leaks)
	}
}

// TestBoundarySurfacesAreFrozen is the guard. Each case names the party on the
// far side, because that is the question a new field has to answer.
func TestBoundarySurfacesAreFrozen(t *testing.T) {
	t.Run("agent config sent to the server", func(t *testing.T) {
		// The agent marshals this into its connection request. The server
		// persists it and serves it back through GET /clients, so everything
		// here is readable by every authenticated API user who can see the
		// client, not only by the server operator.
		assertSurface(t, "agent -> server -> every API user", reflect.TypeOf(clientconfig.Config{}), []string{
			"client.allow_root",
			"client.bind_interface",
			"client.data_dir",
			"client.fallback_servers",
			"client.fingerprint",
			"client.id",
			"client.ip_api_url",
			"client.ip_refresh_min",
			"client.labels.*",
			"client.name",
			"client.remotes",
			"client.require_fingerprint",
			"client.server",
			"client.server_switchback_interval",
			"client.tags",
			"client.tunnel_allowed",
			"client.updates_interval",
			"client.use_hostname",
			"client.use_system_id",
			"connection.hostname",
			"connection.keep_alive",
			"connection.keep_alive_timeout",
			"connection.max_retry_count",
			"connection.max_retry_interval",
			"connection.watchdog_integration",
			"file_reception.enabled",
			"file_reception.protected",
			"interpreter_aliases.*",
			"interpreter_aliases_encodings.*.input_encoding",
			"interpreter_aliases_encodings.*.output_encoding",
			"logging.log_compress",
			"logging.log_level",
			"logging.log_max_age_days",
			"logging.log_max_backups",
			"logging.log_max_size_mb",
			"monitoring.enabled",
			"monitoring.fs_identify_mountpoints_by_device",
			"monitoring.fs_path_exclude",
			"monitoring.fs_path_exclude_recurse",
			"monitoring.fs_type_include",
			"monitoring.interval",
			"monitoring.lan_card.max_speed",
			"monitoring.lan_card.name",
			"monitoring.net_lan",
			"monitoring.net_wan",
			"monitoring.pm_enabled",
			"monitoring.pm_kerneltasks_enabled",
			"monitoring.pm_max_number_processes",
			"monitoring.wan_card.max_speed",
			"monitoring.wan_card.name",
			"remote_commands.allow",
			"remote_commands.allow_regexp",
			"remote_commands.deny",
			"remote_commands.deny_regexp",
			"remote_commands.enabled",
			"remote_commands.order",
			"remote_commands.send_back_limit",
			"remote_scripts.enabled",
		})
	})

	t.Run("connection request sent to the server", func(t *testing.T) {
		assertSurface(t, "agent -> server", reflect.TypeOf(chshare.ConnectionRequest{}), []string{
			"CPUFamily",
			"CPUModel",
			"CPUModelName",
			"CPUVendor",
			"ClientConfiguration.client.allow_root",
			"ClientConfiguration.client.bind_interface",
			"ClientConfiguration.client.data_dir",
			"ClientConfiguration.client.fallback_servers",
			"ClientConfiguration.client.fingerprint",
			"ClientConfiguration.client.id",
			"ClientConfiguration.client.ip_api_url",
			"ClientConfiguration.client.ip_refresh_min",
			"ClientConfiguration.client.labels.*",
			"ClientConfiguration.client.name",
			"ClientConfiguration.client.remotes",
			"ClientConfiguration.client.require_fingerprint",
			"ClientConfiguration.client.server",
			"ClientConfiguration.client.server_switchback_interval",
			"ClientConfiguration.client.tags",
			"ClientConfiguration.client.tunnel_allowed",
			"ClientConfiguration.client.updates_interval",
			"ClientConfiguration.client.use_hostname",
			"ClientConfiguration.client.use_system_id",
			"ClientConfiguration.connection.hostname",
			"ClientConfiguration.connection.keep_alive",
			"ClientConfiguration.connection.keep_alive_timeout",
			"ClientConfiguration.connection.max_retry_count",
			"ClientConfiguration.connection.max_retry_interval",
			"ClientConfiguration.connection.watchdog_integration",
			"ClientConfiguration.file_reception.enabled",
			"ClientConfiguration.file_reception.protected",
			"ClientConfiguration.interpreter_aliases.*",
			"ClientConfiguration.interpreter_aliases_encodings.*.input_encoding",
			"ClientConfiguration.interpreter_aliases_encodings.*.output_encoding",
			"ClientConfiguration.logging.log_compress",
			"ClientConfiguration.logging.log_level",
			"ClientConfiguration.logging.log_max_age_days",
			"ClientConfiguration.logging.log_max_backups",
			"ClientConfiguration.logging.log_max_size_mb",
			"ClientConfiguration.monitoring.enabled",
			"ClientConfiguration.monitoring.fs_identify_mountpoints_by_device",
			"ClientConfiguration.monitoring.fs_path_exclude",
			"ClientConfiguration.monitoring.fs_path_exclude_recurse",
			"ClientConfiguration.monitoring.fs_type_include",
			"ClientConfiguration.monitoring.interval",
			"ClientConfiguration.monitoring.lan_card.max_speed",
			"ClientConfiguration.monitoring.lan_card.name",
			"ClientConfiguration.monitoring.net_lan",
			"ClientConfiguration.monitoring.net_wan",
			"ClientConfiguration.monitoring.pm_enabled",
			"ClientConfiguration.monitoring.pm_kerneltasks_enabled",
			"ClientConfiguration.monitoring.pm_max_number_processes",
			"ClientConfiguration.monitoring.wan_card.max_speed",
			"ClientConfiguration.monitoring.wan_card.name",
			"ClientConfiguration.remote_commands.allow",
			"ClientConfiguration.remote_commands.allow_regexp",
			"ClientConfiguration.remote_commands.deny",
			"ClientConfiguration.remote_commands.deny_regexp",
			"ClientConfiguration.remote_commands.enabled",
			"ClientConfiguration.remote_commands.order",
			"ClientConfiguration.remote_commands.send_back_limit",
			"ClientConfiguration.remote_scripts.enabled",
			"Hostname",
			"ID",
			"IPv4",
			"IPv6",
			"Labels.*",
			"MemoryTotal",
			"Name",
			"NumCPUs",
			"OS",
			"OSArch",
			"OSFamily",
			"OSFullName",
			"OSKernel",
			"OSVersion",
			"OSVirtualizationRole",
			"OSVirtualizationSystem",
			"Remotes.acl",
			"Remotes.auth_password",
			"Remotes.auth_user",
			"Remotes.auto_close",
			"Remotes.host_header",
			"Remotes.http_proxy",
			"Remotes.idle_timeout_minutes",
			"Remotes.lhost",
			"Remotes.lport",
			"Remotes.lport_random",
			"Remotes.name",
			"Remotes.owner",
			"Remotes.protocol",
			"Remotes.rhost",
			"Remotes.rport",
			"Remotes.scheme",
			"Remotes.skip_tls_verify",
			"Remotes.tunnel_url",
			"SessionID",
			"Tags",
			"Timezone",
			"Version",
		})
	})

	t.Run("client payload served by the API", func(t *testing.T) {
		// GET /clients/{id} emits every supported field when the request names
		// none, to any authenticated user with access to that client.
		assertSurface(t, "server -> any authenticated API user", reflect.TypeOf(clients.ClientPayload{}), []string{
			"address",
			"allowed_user_groups",
			"client_auth_id",
			"client_configuration.client.allow_root",
			"client_configuration.client.bind_interface",
			"client_configuration.client.data_dir",
			"client_configuration.client.fallback_servers",
			"client_configuration.client.fingerprint",
			"client_configuration.client.id",
			"client_configuration.client.ip_api_url",
			"client_configuration.client.ip_refresh_min",
			"client_configuration.client.labels.*",
			"client_configuration.client.name",
			"client_configuration.client.remotes",
			"client_configuration.client.require_fingerprint",
			"client_configuration.client.server",
			"client_configuration.client.server_switchback_interval",
			"client_configuration.client.tags",
			"client_configuration.client.tunnel_allowed",
			"client_configuration.client.updates_interval",
			"client_configuration.client.use_hostname",
			"client_configuration.client.use_system_id",
			"client_configuration.connection.hostname",
			"client_configuration.connection.keep_alive",
			"client_configuration.connection.keep_alive_timeout",
			"client_configuration.connection.max_retry_count",
			"client_configuration.connection.max_retry_interval",
			"client_configuration.connection.watchdog_integration",
			"client_configuration.file_reception.enabled",
			"client_configuration.file_reception.protected",
			"client_configuration.interpreter_aliases.*",
			"client_configuration.interpreter_aliases_encodings.*.input_encoding",
			"client_configuration.interpreter_aliases_encodings.*.output_encoding",
			"client_configuration.logging.log_compress",
			"client_configuration.logging.log_level",
			"client_configuration.logging.log_max_age_days",
			"client_configuration.logging.log_max_backups",
			"client_configuration.logging.log_max_size_mb",
			"client_configuration.monitoring.enabled",
			"client_configuration.monitoring.fs_identify_mountpoints_by_device",
			"client_configuration.monitoring.fs_path_exclude",
			"client_configuration.monitoring.fs_path_exclude_recurse",
			"client_configuration.monitoring.fs_type_include",
			"client_configuration.monitoring.interval",
			"client_configuration.monitoring.lan_card.max_speed",
			"client_configuration.monitoring.lan_card.name",
			"client_configuration.monitoring.net_lan",
			"client_configuration.monitoring.net_wan",
			"client_configuration.monitoring.pm_enabled",
			"client_configuration.monitoring.pm_kerneltasks_enabled",
			"client_configuration.monitoring.pm_max_number_processes",
			"client_configuration.monitoring.wan_card.max_speed",
			"client_configuration.monitoring.wan_card.name",
			"client_configuration.remote_commands.allow",
			"client_configuration.remote_commands.allow_regexp",
			"client_configuration.remote_commands.deny",
			"client_configuration.remote_commands.deny_regexp",
			"client_configuration.remote_commands.enabled",
			"client_configuration.remote_commands.order",
			"client_configuration.remote_commands.send_back_limit",
			"client_configuration.remote_scripts.enabled",
			"connection_state",
			"cpu_family",
			"cpu_model",
			"cpu_model_name",
			"cpu_vendor",
			"disconnected_at",
			"ext_ip_addresses.error",
			"ext_ip_addresses.ipv4",
			"ext_ip_addresses.ipv6",
			"ext_ip_addresses.updated_at",
			"groups",
			"hostname",
			"id",
			"ipv4",
			"ipv6",
			"labels.*",
			"last_heartbeat_at",
			"mem_total",
			"name",
			"num_cpus",
			"os",
			"os_arch",
			"os_family",
			"os_full_name",
			"os_kernel",
			"os_version",
			"os_virtualization_role",
			"os_virtualization_system",
			"tags",
			"timezone",
			"tunnels.acl",
			"tunnels.auth_password",
			"tunnels.auth_user",
			"tunnels.auto_close",
			"tunnels.created_at",
			"tunnels.host_header",
			"tunnels.http_proxy",
			"tunnels.id",
			"tunnels.idle_timeout_minutes",
			"tunnels.lhost",
			"tunnels.lport",
			"tunnels.lport_random",
			"tunnels.name",
			"tunnels.owner",
			"tunnels.protocol",
			"tunnels.rhost",
			"tunnels.rport",
			"tunnels.scheme",
			"tunnels.skip_tls_verify",
			"tunnels.tunnel_url",
			"updates_status.error",
			"updates_status.hint",
			"updates_status.reboot_pending",
			"updates_status.refreshed",
			"updates_status.security_updates_available",
			"updates_status.update_summaries.description",
			"updates_status.update_summaries.is_security_update",
			"updates_status.update_summaries.reboot_required",
			"updates_status.update_summaries.title",
			"updates_status.updates_available",
			"version",
		})
	})

	t.Run("tunnel listing served by the API", func(t *testing.T) {
		// GET /tunnels serves every tunnel the caller can see, gated on the
		// tunnels permission rather than on admin.
		assertSurface(t, "server -> any user with the tunnels permission", reflect.TypeOf(TunnelPayload{}), []string{
			"acl",
			"auth_password",
			"auth_user",
			"auto_close",
			"client_id",
			"created_at",
			"host_header",
			"http_proxy",
			"id",
			"idle_timeout_minutes",
			"lhost",
			"lport",
			"lport_random",
			"name",
			"owner",
			"protocol",
			"rhost",
			"rport",
			"scheme",
			"skip_tls_verify",
			"tunnel_url",
		})
	})

	t.Run("agent credential record", func(t *testing.T) {
		assertSurface(t, "server -> admin API user", reflect.TypeOf(clientsauth.ClientAuth{}), []string{
			"id",
			"password",
		})
	})

	t.Run("user record served by the API", func(t *testing.T) {
		assertSurface(t, "server -> API user", reflect.TypeOf(UserPayload{}), []string{
			"effective_extended_permissions.commands_restricted.*",
			"effective_extended_permissions.tunnels_restricted.*",
			"effective_user_permissions.*",
			"group_permissions_enabled",
			"groups",
			"password_expired",
			"two_fa_send_to",
			"username",
		})
	})

	t.Run("client group served by the API", func(t *testing.T) {
		assertSurface(t, "server -> API user", reflect.TypeOf(ClientGroupPayload{}), []string{
			"allowed_user_groups",
			"client_ids",
			"description",
			"id",
			"num_clients",
			"num_clients_connected",
			"params.address",
			"params.client_auth_id",
			"params.client_id",
			"params.connection_state",
			"params.hostname",
			"params.ipv4",
			"params.ipv6",
			"params.name",
			"params.os",
			"params.os_arch",
			"params.os_family",
			"params.os_kernel",
			"params.tag",
			"params.version",
		})
	})

	t.Run("command job served by the API", func(t *testing.T) {
		// Command output is attacker-influenced and often carries whatever the
		// command printed, so this surface is worth watching even though none of
		// its keys are credential-shaped.
		assertSurface(t, "server -> user with the commands permission", reflect.TypeOf(jobPayload{}), []string{
			"client_id",
			"client_name",
			"command",
			"created_by",
			"cwd",
			"error",
			"finished_at",
			"interpreter",
			"is_script",
			"is_sudo",
			"jid",
			"multi_job_id",
			"pid",
			"result.stderr",
			"result.stdout",
			"result.summary",
			"schedule_id",
			"started_at",
			"status",
			"timeout_sec",
		})
	})

	t.Run("login response", func(t *testing.T) {
		assertSurface(t, "server -> the caller who just authenticated", reflect.TypeOf(loginResponse{}), []string{
			"token",
			"two_fa.delivery_method",
			"two_fa.send_to",
			"two_fa.totp_key_status",
		})
	})

	t.Run("pairing deposit response", func(t *testing.T) {
		assertSurface(t, "server -> the caller who requested the pairing", reflect.TypeOf(pairingDepositResponse{}), []string{
			"expires",
			"installers.linux",
			"installers.windows",
			"pairing_code",
		})
	})

	t.Run("tunnel model", func(t *testing.T) {
		// Embedded in the client payload and in the stored client blob.
		assertSurface(t, "server -> API user, and server -> disk", reflect.TypeOf(clienttunnel.Tunnel{}), []string{
			"acl",
			"auth_password",
			"auth_user",
			"auto_close",
			"created_at",
			"host_header",
			"http_proxy",
			"id",
			"idle_timeout_minutes",
			"lhost",
			"lport",
			"lport_random",
			"name",
			"owner",
			"protocol",
			"rhost",
			"rport",
			"scheme",
			"skip_tls_verify",
			"tunnel_url",
		})
	})
}

// TestSecretKeyAllowListIsCurrent keeps the allow list from outliving the fields
// it excuses. A stale entry is a silent hole: it would go on excusing a key that
// came back later under a different meaning.
func TestSecretKeyAllowListIsCurrent(t *testing.T) {
	surfaces := [][]string{
		jsonsurface.Of(reflect.TypeOf(clientconfig.Config{})),
		jsonsurface.Of(reflect.TypeOf(chshare.ConnectionRequest{})),
		jsonsurface.Of(reflect.TypeOf(clients.ClientPayload{})),
		jsonsurface.Of(reflect.TypeOf(clienttunnel.Tunnel{})),
		jsonsurface.Of(reflect.TypeOf(TunnelPayload{})),
		jsonsurface.Of(reflect.TypeOf(clientsauth.ClientAuth{})),
		jsonsurface.Of(reflect.TypeOf(UserPayload{})),
		jsonsurface.Of(reflect.TypeOf(ClientGroupPayload{})),
		jsonsurface.Of(reflect.TypeOf(jobPayload{})),
		jsonsurface.Of(reflect.TypeOf(loginResponse{})),
		jsonsurface.Of(reflect.TypeOf(pairingDepositResponse{})),
	}

	for key := range secretKeyAllowList {
		found := false
		for _, keys := range surfaces {
			for _, k := range keys {
				if k == key {
					found = true
					break
				}
			}
		}
		require.True(t, found,
			"secretKeyAllowList excuses %q, which is no longer on any boundary surface. "+
				"Delete the entry rather than leave it to excuse a future field of the same name.",
			key)
	}
}
