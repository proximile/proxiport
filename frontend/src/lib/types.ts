/** Shape of the bits of the API response we actually consume. */

export type Client = {
  id: string;
  name: string;
  hostname?: string;
  os?: string;
  os_full_name?: string;
  os_kernel?: string;
  os_arch?: string;
  os_family?: string;
  os_version?: string;
  num_cpus?: number;
  mem_total?: number;
  cpu_model?: string;
  cpu_vendor?: string;
  cpu_family?: string;
  ipv4?: string[];
  ipv6?: string[];
  tags?: string[];
  labels?: Record<string, string>;
  groups?: string[];
  version?: string;
  address?: string;
  connection_state?: 'connected' | 'disconnected';
  disconnected_at?: string | null;
  last_heartbeat_at?: string | null;
  client_auth_id?: string;
  tunnels?: Tunnel[];
  updates_status?: { updates_summary?: { total: number; updates_count: number; security_updates_count: number } };
};

export type Tunnel = {
  id?: string;
  client_id?: string;
  lhost?: string;
  lport?: string;
  rhost?: string;
  rport?: string;
  scheme?: string;
  protocol?: string;
  acl?: string;
  idle_timeout_minutes?: number;
  http_proxy?: boolean;
  is_reverse_proxy?: boolean;
  created_at?: string;
};

export type User = {
  username: string;
  groups?: string[];
  two_fa_send_to?: string;
  effective_user_permissions?: Record<string, boolean>;
  effective_extended_permissions?: Record<string, unknown>;
};

export type Group = {
  name: string;
  permissions?: Record<string, boolean>;
};

export type ClientAuthEntry = {
  id: string;
  password: string;
};

export type ClientGroup = {
  id: string;
  description?: string;
  params?: Record<string, unknown>;
  num_clients?: number;
  allowed_user_groups?: string[];
};

export type AuditEntry = {
  id?: number;
  timestamp: string;
  username?: string;
  remote_ip?: string;
  application?: string;
  action?: string;
  affected_id?: string;
  client_id?: string;
  client_hostname?: string;
  request?: string;
  response?: string;
};

export type Job = {
  jid: string;
  client_id: string;
  command?: string;
  interpreter?: string;
  status?: string;
  result?: { stdout: string; stderr: string; summary: string };
  error?: string;
  created_by?: string;
  is_sudo?: boolean;
  cwd?: string;
  pid?: number;
  started_at?: string;
  finished_at?: string;
};

export type Schedule = {
  id?: string;
  name?: string;
  type?: string;
  schedule?: string;
  client_ids?: string[];
  group_ids?: string[];
  details?: Record<string, unknown>;
  created_at?: string;
  created_by?: string;
};

export type ApiToken = {
  prefix: string;
  name?: string;
  scope?: string;
  expires_at?: string;
  created_at?: string;
  last_used_at?: string;
};

export type ServerStatus = {
  version?: string;
  fingerprint?: string;
  connect_url?: string[];
  pairing_url?: string;
  clients_connected?: number;
  clients_disconnected?: number;
  two_fa_enabled?: boolean;
  two_fa_delivery_method?: string;
  clients_auth_source?: string;
  clients_auth_mode?: string;
  users_auth_source?: string;
  auth_provider_settings?: Record<string, unknown>;
};
