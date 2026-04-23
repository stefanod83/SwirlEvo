// TS mirror of biz/compose_stack_addons_yaml.go buildTraefikLabels. The
// backend emits the canonical form on save; this helper only feeds the
// live preview inside the wizard tab, so minor cosmetic drift between
// the two emitters is harmless.
import type { TraefikServiceCfg } from '@/api/compose_stack'

export function buildTraefikLabels(_svc: string, cfg: TraefikServiceCfg): Record<string, string> {
  const out: Record<string, string> = {}
  if (cfg.labels) {
    for (const k of Object.keys(cfg.labels)) {
      if (k.startsWith('traefik.')) out[k] = cfg.labels[k]
    }
  }
  if (cfg.enabled) out['traefik.enable'] = 'true'
  return out
}
