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

export function test(endpoint: string) {
  return ajax.post('/host/test', { endpoint })
}

export function sync(id: string) {
  return ajax.post('/host/sync', { id })
}
