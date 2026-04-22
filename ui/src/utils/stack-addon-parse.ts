// Shared helpers for the stack editor addon wizards. Parsing is read-only:
// the frontend never emits YAML — the Go backend holds the single authoritative
// emitter. js-yaml is used here purely to extract structured information from
// the user's compose content (service names, label map introspection, uploaded
// traefik.yml on the Traefik tab).
import yaml from 'js-yaml'

export type LoadedCompose = {
  services?: Record<string, any>
  [k: string]: any
}

// parseComposeDoc returns a best-effort parsed document or null if the YAML is
// unparsable. Callers must tolerate null (the user may be mid-typing).
export function parseComposeDoc(content: string): LoadedCompose | null {
  try {
    const doc = yaml.load(content ?? '', { schema: yaml.DEFAULT_SCHEMA })
    if (doc && typeof doc === 'object') return doc as LoadedCompose
    return null
  } catch {
    return null
  }
}

// parseServiceNames returns the top-level service keys of a compose document.
// Replaces the line-scanning heuristic previously used in the stack editor —
// js-yaml handles anchors, multi-line strings, and indentation variants that
// the regex could not.
export function parseServiceNames(content: string): string[] {
  const doc = parseComposeDoc(content)
  if (!doc || !doc.services || typeof doc.services !== 'object') return []
  return Object.keys(doc.services)
}

// collectManagedLabels walks the parsed compose doc and extracts, for each
// service, the labels carrying the `# swirl-managed` trailing comment in
// the source YAML. Since js-yaml's default SCHEMA drops line-comments, we
// resort to a regex sweep on the raw text to flag the marker-bearing keys
// and only then surface their values from the parsed map.
//
// The wizard tabs feed the result into their "reverse parser" helpers
// (traefikCfgFromLabels etc.) to rebuild the tab state when an existing
// stack is re-opened.
export function collectManagedLabels(content: string): Record<string, Record<string, string>> {
  const doc = parseComposeDoc(content)
  const out: Record<string, Record<string, string>> = {}
  if (!doc?.services || typeof doc.services !== 'object') return out
  // Parse a "managed key" set from raw text: any `KEY: VALUE # swirl-managed`
  // line qualifies. Doesn't need to be tightly scoped to service — service
  // inference below filters by actual label-map membership.
  const managedKeys = scanMarkedKeys(content)
  if (!managedKeys.size) return out
  for (const svc of Object.keys(doc.services)) {
    const svcNode = (doc.services as any)[svc] || {}
    const buckets = [svcNode.labels, svcNode?.deploy?.labels]
    const picked: Record<string, string> = {}
    for (const bucket of buckets) {
      if (!bucket || typeof bucket !== 'object') continue
      for (const k of Object.keys(bucket)) {
        if (!managedKeys.has(k)) continue
        const v = (bucket as any)[k]
        picked[k] = v == null ? '' : String(v)
      }
    }
    if (Object.keys(picked).length) out[svc] = picked
  }
  return out
}

// scanMarkedKeys returns the set of label keys that have a trailing
// `# swirl-managed` comment on their line in the raw YAML. Kept deliberately
// loose: handles both `key: value # swirl-managed` and `"key": value # ...`
// without parsing YAML itself — the key name is always what precedes the
// first `:` on the line.
const MARKED_LINE = /^\s*([A-Za-z0-9_.\-"]+)\s*:.*#\s*swirl-managed/
function scanMarkedKeys(content: string): Set<string> {
  const set = new Set<string>()
  for (const line of (content || '').split('\n')) {
    const m = MARKED_LINE.exec(line)
    if (!m) continue
    const key = m[1].replace(/^"|"$/g, '')
    set.add(key)
  }
  return set
}
