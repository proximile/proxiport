export const APP_NAME = 'ProxiPort';

/** Section names for the top-level app pages, keyed by pathname. */
const SECTION_TITLES: Record<string, string> = {
  '/inventory': 'Inventory',
  '/tunnels': 'Tunnels',
  '/commands': 'Commands',
  '/commands/library': 'Library',
  '/commands/schedules': 'Schedules',
  '/scripts': 'Scripts',
  '/documentation': 'Documentation',
  '/settings/audit': 'Audit',
  '/settings/users': 'Users',
  '/settings/user-groups': 'User Groups',
  '/settings/client-access': 'Client Access',
  '/settings/client-groups': 'Client Groups',
  '/settings/vault': 'Vault',
  '/settings/api-tokens': 'API Tokens',
  '/settings/info': 'Info',
  '/settings/support': 'Support',
  '/settings/profile': 'Profile'
};

/** Join title parts most-specific first and end with the product name. */
export function documentTitle(...parts: Array<string | null | undefined>): string {
  return [...parts.filter((p): p is string => !!p), APP_NAME].join(' · ');
}

/**
 * Document title for a top-level app page, e.g. "Vault · ProxiPort".
 * Returns null for a client's detail pages (/inventory/<id>/...), whose
 * layout sets a title naming the client and the open tab.
 */
export function sectionTitle(pathname: string): string | null {
  const path = pathname.length > 1 ? pathname.replace(/\/+$/, '') : pathname;
  if (path.startsWith('/inventory/')) return null;
  const section = SECTION_TITLES[path];
  return section ? documentTitle(section) : APP_NAME;
}
