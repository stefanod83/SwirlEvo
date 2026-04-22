// TS mirror of biz/compose_stack_addons_yaml.go buildTraefikLabels.
// Used for live preview in the wizard tabs — the canonical emitter lives on
// the Go side and is re-run at Save time, so cosmetic drift between the two
// doesn't risk corrupting the persisted YAML.
import type { TraefikServiceCfg } from '@/api/compose_stack'

export function buildTraefikLabels(svc: string, cfg: TraefikServiceCfg): Record<string, string> {
  // Start from the passthrough set. Structured fields below override
  // on key collision — the wizard always wins over the extras, by design.
  const out: Record<string, string> = {}
  if (cfg.extraLabels) {
    for (const k of Object.keys(cfg.extraLabels)) {
      if (k.startsWith('traefik.')) out[k] = cfg.extraLabels[k]
    }
  }
  if (!cfg.enabled) return out
  const router = (cfg.router || svc).trim() || svc
  const rule = buildTraefikRule(cfg)
  if (!rule || !cfg.port || cfg.port <= 0) return out
  out['traefik.enable'] = 'true'
  out[`traefik.http.routers.${router}.rule`] = rule
  out[`traefik.http.services.${router}.loadbalancer.server.port`] = String(cfg.port)
  if (cfg.entrypoint) out[`traefik.http.routers.${router}.entrypoints`] = cfg.entrypoint
  if (cfg.tls) {
    out[`traefik.http.routers.${router}.tls`] = 'true'
    if (cfg.certResolver) out[`traefik.http.routers.${router}.tls.certresolver`] = cfg.certResolver
  }
  if (cfg.middlewares && cfg.middlewares.length) {
    out[`traefik.http.routers.${router}.middlewares`] = cfg.middlewares.join(',')
  }
  return out
}

function buildTraefikRule(cfg: TraefikServiceCfg): string {
  const domain = (cfg.domain || '').trim()
  const path = (cfg.path || '').trim()
  const kind = (cfg.ruleType || '').toLowerCase()
  switch (kind) {
    case 'host':
      return domain ? `Host(\`${domain}\`)` : ''
    case 'pathprefix':
      return path ? `PathPrefix(\`${path}\`)` : ''
    case 'host+pathprefix':
      if (!domain || !path) return ''
      return `Host(\`${domain}\`) && PathPrefix(\`${path}\`)`
    default:
      return domain ? `Host(\`${domain}\`)` : ''
  }
}

// Reverse parse: rebuild the wizard cfg from a set of already-swirl-managed
// labels on a service. Mirrors Go traefikCfgFromLabels.
export function traefikCfgFromLabels(labels: Record<string, string>): TraefikServiceCfg {
  const cfg: TraefikServiceCfg = { enabled: false }
  if (labels['traefik.enable'] !== 'true') return cfg
  cfg.enabled = true
  let router = ''
  for (const k of Object.keys(labels)) {
    if (k.startsWith('traefik.http.routers.')) {
      const rest = k.slice('traefik.http.routers.'.length)
      router = rest.split('.', 2)[0] || ''
      if (router) break
    }
  }
  cfg.router = router
  const rule = labels[`traefik.http.routers.${router}.rule`]
  if (rule) {
    const parsed = parseTraefikRule(rule)
    cfg.ruleType = parsed.kind as any
    cfg.domain = parsed.domain
    cfg.path = parsed.path
  }
  const ep = labels[`traefik.http.routers.${router}.entrypoints`]
  if (ep) cfg.entrypoint = ep
  const portStr = labels[`traefik.http.services.${router}.loadbalancer.server.port`]
  if (portStr) cfg.port = parseInt(portStr, 10) || 0
  const tls = labels[`traefik.http.routers.${router}.tls`]
  if (tls === 'true') cfg.tls = true
  const cr = labels[`traefik.http.routers.${router}.tls.certresolver`]
  if (cr) cfg.certResolver = cr
  const mws = labels[`traefik.http.routers.${router}.middlewares`]
  if (mws) cfg.middlewares = mws.split(',').filter(Boolean)
  return cfg
}

function parseTraefikRule(rule: string): { kind: string; domain: string; path: string } {
  const trimmed = rule.trim()
  const both = /^Host\(`([^`]+)`\)\s*&&\s*PathPrefix\(`([^`]+)`\)$/.exec(trimmed)
  if (both) return { kind: 'Host+PathPrefix', domain: both[1], path: both[2] }
  const host = /^Host\(`([^`]+)`\)$/.exec(trimmed)
  if (host) return { kind: 'Host', domain: host[1], path: '' }
  const path = /^PathPrefix\(`([^`]+)`\)$/.exec(trimmed)
  if (path) return { kind: 'PathPrefix', domain: '', path: path[1] }
  return { kind: '', domain: '', path: '' }
}
