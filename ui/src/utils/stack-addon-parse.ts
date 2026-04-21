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
