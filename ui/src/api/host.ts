import ajax from './ajax'

export interface Host {
  id: string
  name: string
  endpoint: string
  authMethod: string
  tlsCaCert?: string
  tlsCert?: string
  sshUser?: string
  // Optional hex colour (#rrggbb). Shown as a tag in the Hosts list,
  // as a 3px-wide strip in the HostSelector dropdown, and as a 4px
  // bar under the page header when this host is the active selection.
  color?: string
  status: string
  error?: string
  engineVersion?: string
  os?: string
  arch?: string
  cpus?: number
  memory?: number
  createdAt: number
  updatedAt: number
  createdBy: { id: string; name: string }
  updatedBy: { id: string; name: string }
}

export interface HostInfo {
  engineVersion: string
  os: string
  arch: string
  cpus: number
  memory: number
  hostname: string
}

export function search(name?: string, status?: string, pageIndex?: number, pageSize?: number) {
  return ajax.get('/host/search', { name, status, pageIndex, pageSize })
}

export function find(id: string) {
  return ajax.get('/host/find', { id })
}

export function save(host: Partial<Host>) {
  return ajax.post('/host/save', host)
}

export function remove(id: string, name: string) {
  return ajax.post('/host/delete', { id, name })
}

export function test(endpoint: string, authMethod?: string) {
  return ajax.post('/host/test', { endpoint, authMethod: authMethod || '' })
}

export function sync(id: string) {
  return ajax.post('/host/sync', { id })
}

// Addon config extract — persisted JSON blob scoped to a host. Curated
// from the Host edit page: lists extracted from an uploaded addon config
// file (e.g. traefik.yml), pointers to the stack/container that serves the
// addon, default values pre-filled by the stack-editor wizard tab, and
// free-form overrides for fields the structured form doesn't capture.
export interface TraefikExtract {
  enabled?: boolean
  entryPoints?: string[]
  certResolvers?: string[]
  middlewares?: string[]
  networks?: string[]
  stackId?: string
  containerName?: string
  defaultDomain?: string
  defaultEntrypoint?: string
  defaultCertResolver?: string
  overrides?: Record<string, string>
  sourceFile?: string
  uploadedAt?: string
  uploadedBy?: string
}

export interface AddonConfigExtract {
  traefik?: TraefikExtract
}

export function getAddonExtract(hostId: string) {
  return ajax.get<AddonConfigExtract>('/host/addon-extract-get', { hostId })
}

export function saveAddonExtract(hostId: string, extract: AddonConfigExtract) {
  return ajax.post('/host/addon-extract-save', { hostId, extract })
}

export function clearAddonExtract(hostId: string, addon?: string) {
  return ajax.post('/host/addon-extract-clear', { hostId, addon: addon || '' })
}
