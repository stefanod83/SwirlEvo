import { h } from 'vue'
import { NTag } from 'naive-ui'

// Badge metadata shared between the stack list (active addons per stack)
// and the host list (enabled addons per host). One colour/label mapping
// so the UI reads consistently wherever addons are rendered.
export const ADDON_BADGE_META: Record<
  string,
  { label: string; type: 'info' | 'success' | 'warning' | 'default' | 'error' }
> = {
  traefik:          { label: 'Traefik',        type: 'info' },
  sablier:          { label: 'Sablier',        type: 'warning' },
  watchtower:       { label: 'Watchtower',     type: 'default' },
  backup:           { label: 'Backup',         type: 'success' },
  resources:        { label: 'Resources',      type: 'warning' },
  'registry-cache': { label: 'Registry Cache', type: 'info' },
  registryCache:    { label: 'Registry Cache', type: 'info' },
}

// renderAddonBadges turns a list of addon keys into a row of compact
// chips. Returns null when the list is empty so callers can skip
// the wrapper element and keep layout tight.
export function renderAddonBadges(addons: string[] | undefined) {
  if (!addons || !addons.length) return null
  const chips = addons.map((a) => {
    const meta = ADDON_BADGE_META[a] || { label: a, type: 'default' as const }
    return h(
      NTag,
      {
        size: 'small',
        type: meta.type,
        round: true,
        bordered: false,
        style: 'font-size: 10px; padding: 0 6px; line-height: 16px; height: 16px',
      },
      { default: () => meta.label },
    )
  })
  return h('span', { style: 'display: inline-flex; gap: 4px; flex-wrap: wrap' }, chips)
}
