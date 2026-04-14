import { store } from '@/store'
import nodeApi from '@/api/node'
import * as hostApi from '@/api/host'

export interface HostNodeOption {
  label: string;
  value: string;
}

// listHostsOrNodes returns a list of {label,value} suitable for an <n-select>
// populated from hosts (standalone mode) or swarm nodes (swarm mode).
export async function listHostsOrNodes(): Promise<HostNodeOption[]> {
  if (store.state.mode === 'standalone') {
    const r = await hostApi.search('', '', 1, 1000)
    const data = r.data as any
    return (data?.items || []).map((h: any) => ({ label: h.name, value: h.id }))
  }
  const r = await nodeApi.list(true)
  return (r.data || []).map((n: any) => ({ label: n.name, value: n.id }))
}
