// Schema definitions consumed by StructuredLabelsEditor. One per
// addon. The editor stays generic — each addon only has to describe
// its label-to-row mapping, key catalogue and default seeds.
//
// Conventions:
//   - `labelFromRow` returns "" for incomplete rows so the builder
//     skips them (the UI flags them as dirty until filled).
//   - `rowFromLabel` must never lose a label: unknown shapes fall
//     through to { section: 'custom', key: <full>, value: ... }.
//   - Defaults use the compose service name as the router/service/
//     middleware name so the generated set is self-consistent.

import type { StructuredRow, EditorSchema } from '@/components/stack-addons/StructuredLabelsEditor.vue'

// --------------------------------------------------------------------
// Traefik
// --------------------------------------------------------------------

const TRAEFIK_ROUTER_KEYS = [
  'rule', 'entrypoints', 'priority', 'service', 'middlewares',
  'tls', 'tls.certresolver', 'tls.options',
  'tls.domains[0].main', 'tls.domains[0].sans',
]
const TRAEFIK_SERVICE_KEYS = [
  'loadbalancer.server.port',
  'loadbalancer.server.scheme',
  'loadbalancer.passhostheader',
  'loadbalancer.sticky.cookie.name',
  'loadbalancer.healthcheck.path',
  'loadbalancer.healthcheck.interval',
]
const TRAEFIK_MIDDLEWARE_KEYS = [
  'basicauth.users',
  'basicauth.realm',
  'stripprefix.prefixes',
  'redirectregex.regex',
  'redirectregex.replacement',
  'redirectscheme.scheme',
  'ratelimit.average',
  'headers.customresponseheaders.X-Frame-Options',
  'headers.sslredirect',
  'compress',
]

export function makeTraefikSchema(
  entrypointOpts: () => { label: string; value: string }[],
  certResolverOpts: () => { label: string; value: string }[],
  middlewareOpts: () => { label: string; value: string }[],
  defaults: { domain?: string; entrypoint?: string; certResolver?: string; middleware?: string },
): EditorSchema {
  return {
    prefix: 'traefik.',
    enableKey: 'traefik.enable',
    sections: [
      { label: 'router', value: 'router' },
      { label: 'service', value: 'service' },
      { label: 'middleware', value: 'middleware' },
      { label: 'traefik.enable', value: 'enable' },
      { label: 'custom', value: 'custom' },
    ],
    keyCatalogue: {
      router: TRAEFIK_ROUTER_KEYS,
      service: TRAEFIK_SERVICE_KEYS,
      middleware: TRAEFIK_MIDDLEWARE_KEYS,
      enable: [],
      custom: [],
    },
    nameEditable: (section) => section === 'router' || section === 'service' || section === 'middleware',
    labelFromRow: (r) => {
      switch (r.section) {
        case 'enable':     return r.value === 'true' ? 'traefik.enable' : ''
        case 'router':     return r.name && r.key ? `traefik.http.routers.${r.name}.${r.key}` : ''
        case 'service':    return r.name && r.key ? `traefik.http.services.${r.name}.${r.key}` : ''
        case 'middleware': return r.name && r.key ? `traefik.http.middlewares.${r.name}.${r.key}` : ''
        case 'custom':     return r.key.trim()
      }
      return ''
    },
    rowFromLabel: (k, v) => {
      if (k === 'traefik.enable') return { section: 'enable', name: '', key: 'traefik.enable', value: v }
      const pick = (prefix: string, section: string): StructuredRow | null => {
        if (!k.startsWith(prefix)) return null
        const rest = k.slice(prefix.length)
        const dot = rest.indexOf('.')
        if (dot <= 0) return null
        return { section, name: rest.slice(0, dot), key: rest.slice(dot + 1), value: v }
      }
      return (
        pick('traefik.http.routers.', 'router') ||
        pick('traefik.http.services.', 'service') ||
        pick('traefik.http.middlewares.', 'middleware') ||
        { section: 'custom', name: '', key: k, value: v }
      )
    },
    isBoolRow: (r) =>
      r.section === 'enable' ||
      (r.section === 'router' && (r.key === 'tls' || r.key === 'loadbalancer.passhostheader')),
    isMultiValueRow: (r) => r.section === 'router' && r.key === 'middlewares',
    hasValueOptions: (r) =>
      (r.section === 'router' && (r.key === 'entrypoints' || r.key === 'tls.certresolver' || r.key === 'middlewares')),
    valueOptionsFor: (r) => {
      if (r.section !== 'router') return []
      if (r.key === 'entrypoints') return entrypointOpts()
      if (r.key === 'tls.certresolver') return certResolverOpts()
      if (r.key === 'middlewares') return middlewareOpts()
      return []
    },
    validateStatus: (r) => {
      if (r.section !== 'service' || r.key !== 'loadbalancer.server.port') return undefined
      const v = (r.value || '').trim()
      if (!v) return undefined
      if (/^[0-9]+$/.test(v)) {
        const n = parseInt(v, 10)
        return n >= 1 && n <= 65535 ? 'success' : 'warning'
      }
      return 'warning'
    },
    defaultRowsFor: (svc) => {
      const rows: StructuredRow[] = [
        { section: 'router', name: svc, key: 'rule', value: `Host(\`${defaults.domain || 'example.com'}\`)` },
        { section: 'router', name: svc, key: 'tls', value: 'true' },
        { section: 'service', name: svc, key: 'loadbalancer.server.port', value: '' },
      ]
      if (defaults.entrypoint) rows.push({ section: 'router', name: svc, key: 'entrypoints', value: defaults.entrypoint })
      if (defaults.certResolver) rows.push({ section: 'router', name: svc, key: 'tls.certresolver', value: defaults.certResolver })
      if (defaults.middleware) rows.push({ section: 'router', name: svc, key: 'middlewares', value: defaults.middleware })
      return rows
    },
  }
}

// --------------------------------------------------------------------
// Sablier — flat labels read by the Sablier daemon via docker provider.
// Everything sits directly under `sablier.<key>`; no nested name layer.
// --------------------------------------------------------------------

const SABLIER_KEYS = [
  'group', 'session_duration', 'strategy', 'theme', 'display_name',
  'blocking_timeout', 'dynamic_refresh_frequency',
]
const SABLIER_STRATEGIES = ['blocking', 'dynamic'].map((v) => ({ label: v, value: v }))

export function makeSablierSchema(
  defaults: { sessionDuration?: string; strategy?: string },
): EditorSchema {
  return {
    prefix: 'sablier.',
    enableKey: 'sablier.enable',
    sections: [
      { label: 'label', value: 'label' },
      { label: 'sablier.enable', value: 'enable' },
      { label: 'custom', value: 'custom' },
    ],
    keyCatalogue: {
      label: SABLIER_KEYS,
      enable: [],
      custom: [],
    },
    nameEditable: () => false,
    labelFromRow: (r) => {
      switch (r.section) {
        case 'enable': return r.value === 'true' ? 'sablier.enable' : ''
        case 'label':  return r.key ? `sablier.${r.key}` : ''
        case 'custom': return r.key.trim()
      }
      return ''
    },
    rowFromLabel: (k, v) => {
      if (k === 'sablier.enable') return { section: 'enable', name: '', key: 'sablier.enable', value: v }
      if (k.startsWith('sablier.')) return { section: 'label', name: '', key: k.slice('sablier.'.length), value: v }
      return { section: 'custom', name: '', key: k, value: v }
    },
    isBoolRow: (r) => r.section === 'enable',
    hasValueOptions: (r) => r.section === 'label' && r.key === 'strategy',
    valueOptionsFor: (r) =>
      r.section === 'label' && r.key === 'strategy' ? SABLIER_STRATEGIES : [],
    defaultRowsFor: (_svc) => {
      const rows: StructuredRow[] = []
      if (defaults.sessionDuration) {
        rows.push({ section: 'label', name: '', key: 'session_duration', value: defaults.sessionDuration })
      } else {
        rows.push({ section: 'label', name: '', key: 'session_duration', value: '30m' })
      }
      rows.push({ section: 'label', name: '', key: 'strategy', value: defaults.strategy || 'dynamic' })
      return rows
    },
  }
}

// --------------------------------------------------------------------
// Watchtower — flat labels under com.centurylinklabs.watchtower.*
// --------------------------------------------------------------------

const WATCHTOWER_PREFIX = 'com.centurylinklabs.watchtower.'
const WATCHTOWER_KEYS = [
  'monitor-only', 'no-pull', 'scope',
  'depends-on',
  'lifecycle.pre-update', 'lifecycle.post-update',
  'lifecycle.pre-check', 'lifecycle.post-check',
]

export function makeWatchtowerSchema(
  defaults: { monitorOnly?: boolean; scope?: string },
): EditorSchema {
  return {
    prefix: WATCHTOWER_PREFIX,
    enableKey: WATCHTOWER_PREFIX + 'enable',
    sections: [
      { label: 'label', value: 'label' },
      { label: 'watchtower.enable', value: 'enable' },
      { label: 'custom', value: 'custom' },
    ],
    keyCatalogue: {
      label: WATCHTOWER_KEYS,
      enable: [],
      custom: [],
    },
    nameEditable: () => false,
    labelFromRow: (r) => {
      switch (r.section) {
        case 'enable': return r.value === 'true' ? WATCHTOWER_PREFIX + 'enable' : ''
        case 'label':  return r.key ? WATCHTOWER_PREFIX + r.key : ''
        case 'custom': return r.key.trim()
      }
      return ''
    },
    rowFromLabel: (k, v) => {
      if (k === WATCHTOWER_PREFIX + 'enable') {
        return { section: 'enable', name: '', key: WATCHTOWER_PREFIX + 'enable', value: v }
      }
      if (k.startsWith(WATCHTOWER_PREFIX)) {
        return { section: 'label', name: '', key: k.slice(WATCHTOWER_PREFIX.length), value: v }
      }
      return { section: 'custom', name: '', key: k, value: v }
    },
    isBoolRow: (r) =>
      r.section === 'enable' ||
      (r.section === 'label' && (r.key === 'monitor-only' || r.key === 'no-pull')),
    defaultRowsFor: (_svc) => {
      const rows: StructuredRow[] = []
      if (defaults.monitorOnly) rows.push({ section: 'label', name: '', key: 'monitor-only', value: 'true' })
      if (defaults.scope) rows.push({ section: 'label', name: '', key: 'scope', value: defaults.scope })
      return rows
    },
  }
}

// --------------------------------------------------------------------
// Backup (docker-backup-containers) — base labels + plugin-specific
// sub-namespaces. The section names map to the label structure:
//   base         → backup.<key>
//   plugin:<p>   → backup.<p>.<key>
// --------------------------------------------------------------------

const BACKUP_BASE_KEYS = [
  'schedule', 'skip-schedule', 'state', 'skip-volumes', 'volumes',
  'auto-fallback', 'plugin', 'plugin-once',
  'retention', 'retention.daily', 'retention.weekly', 'retention.monthly',
]
const BACKUP_PLUGINS = ['mysql', 'postgres', 'mailcow', 'gitlab', 'keycloak', 'registry-gc', 'icinga', 'skip']
const BACKUP_PLUGIN_KEYS: Record<string, string[]> = {
  mysql: [
    'databases', 'all-databases', 'username', 'password-env',
    'include-routines', 'include-triggers', 'single-transaction',
    'portable', 'lock-tables', 'auto-fix-permissions',
  ],
  postgres: ['databases', 'username', 'password-env', 'portable'],
  mailcow: ['root'],
  gitlab: ['backup-dir'],
}
const BACKUP_SCHEDULES = ['daily', 'weekly', 'monthly', 'all']
  .map((v) => ({ label: v, value: v }))
const BACKUP_STATES = ['none', 'pause', 'stop']
  .map((v) => ({ label: v, value: v }))

export function makeBackupSchema(
  defaults: { schedule?: string; plugin?: string },
): EditorSchema {
  const pluginSections = BACKUP_PLUGINS.map((p) => ({ label: `plugin:${p}`, value: `plugin:${p}` }))
  const sections = [
    { label: 'base', value: 'base' },
    ...pluginSections,
    { label: 'backup.enable', value: 'enable' },
    { label: 'custom', value: 'custom' },
  ]
  const keyCatalogue: Record<string, string[]> = {
    base: BACKUP_BASE_KEYS,
    enable: [],
    custom: [],
  }
  for (const p of BACKUP_PLUGINS) {
    keyCatalogue[`plugin:${p}`] = BACKUP_PLUGIN_KEYS[p] || []
  }
  return {
    prefix: 'backup.',
    enableKey: 'backup.enable',
    sections,
    keyCatalogue,
    nameEditable: () => false,
    labelFromRow: (r) => {
      if (r.section === 'enable') return r.value === 'true' ? 'backup.enable' : ''
      if (r.section === 'custom') return r.key.trim()
      if (r.section === 'base')   return r.key ? `backup.${r.key}` : ''
      if (r.section.startsWith('plugin:')) {
        const plugin = r.section.slice('plugin:'.length)
        return r.key ? `backup.${plugin}.${r.key}` : ''
      }
      return ''
    },
    rowFromLabel: (k, v) => {
      if (k === 'backup.enable') return { section: 'enable', name: '', key: 'backup.enable', value: v }
      if (!k.startsWith('backup.')) return { section: 'custom', name: '', key: k, value: v }
      const rest = k.slice('backup.'.length)
      const firstDot = rest.indexOf('.')
      if (firstDot > 0) {
        const head = rest.slice(0, firstDot)
        const tail = rest.slice(firstDot + 1)
        if (BACKUP_PLUGINS.includes(head)) {
          return { section: `plugin:${head}`, name: '', key: tail, value: v }
        }
      }
      return { section: 'base', name: '', key: rest, value: v }
    },
    isBoolRow: (r) =>
      r.section === 'enable' ||
      (r.section === 'base' && (r.key === 'skip-schedule' || r.key === 'skip-volumes' ||
        r.key === 'auto-fallback' || r.key === 'plugin-once')) ||
      (r.section.startsWith('plugin:') && (
        r.key === 'all-databases' || r.key === 'include-routines' ||
        r.key === 'include-triggers' || r.key === 'single-transaction' ||
        r.key === 'portable' || r.key === 'lock-tables' || r.key === 'auto-fix-permissions'
      )),
    hasValueOptions: (r) =>
      (r.section === 'base' && r.key === 'schedule') ||
      (r.section === 'base' && r.key === 'state') ||
      (r.section === 'base' && r.key === 'plugin'),
    valueOptionsFor: (r) => {
      if (r.section === 'base' && r.key === 'schedule') return BACKUP_SCHEDULES
      if (r.section === 'base' && r.key === 'state') return BACKUP_STATES
      if (r.section === 'base' && r.key === 'plugin') {
        return BACKUP_PLUGINS.map((p) => ({ label: p, value: p }))
      }
      return []
    },
    defaultRowsFor: (_svc) => {
      const rows: StructuredRow[] = [
        { section: 'base', name: '', key: 'schedule', value: defaults.schedule || 'daily' },
      ]
      if (defaults.plugin) {
        rows.push({ section: 'base', name: '', key: 'plugin', value: defaults.plugin })
      }
      return rows
    },
  }
}
