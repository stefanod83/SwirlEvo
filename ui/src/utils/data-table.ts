import { computed, isRef, onMounted, reactive, ref } from "vue"
import { t } from "@/locales";

const PAGE_SIZE_KEY = 'tablePageSize'

function loadPageSize(): number {
    const raw = localStorage.getItem(PAGE_SIZE_KEY)
    const n = raw ? parseInt(raw, 10) : 0
    return n > 0 ? n : 10
}

function savePageSize(n: number) {
    try { localStorage.setItem(PAGE_SIZE_KEY, String(n)) } catch {}
}

// SorterState represents the payload emitted by NDataTable's
// `@update:sorter`. When `remote=true`, Naive UI does NOT run the column's
// `sorter` function internally — it merely reports which column the user
// clicked and in which order. The consumer is expected to re-order (or
// re-fetch) the data itself.
//
// In GLOBAL CLIENT-SIDE mode (the new default for list pages in Swirl —
// see `useDataTable` below, invoked with the `remote=false` flag), the
// loader is called ONCE without `pageIndex`/`pageSize`, returning the
// full dataset. Sort is applied across the entire dataset and pagination
// is a pure display slice computed from `sortedData`. This is what makes
// sort "global" rather than "per visible page" — clicking the Name
// header orders every row the server has, not just the 10 in view.
export interface SorterState {
    columnKey: string | number
    // "ascend" | "descend" | false
    order: 'ascend' | 'descend' | false
    // Naive UI also sends the sorter function here, but we look it up from
    // the columns definition to keep a single source of truth.
    sorter?: any
}

export interface UseDataTableOptions {
    // When true (default historical behaviour), the loader is called for
    // every page change with `{pageIndex, pageSize, ...filter}`; the
    // backend returns ONLY that page. Sort in this mode is best-effort
    // per-page. Keep this only for endpoints that stream unbounded data
    // (none today — all 10+ Docker-backed endpoints and all DAO-backed
    // endpoints with a bounded dataset have been migrated to remote=false).
    //
    // When false, the loader is called ONCE on mount / refresh / filter
    // change — without `pageIndex`/`pageSize`. The full dataset is kept
    // in memory. Sort runs on the whole dataset; pagination slices
    // `sortedData` client-side. This gives users GLOBAL sort at the cost
    // of a single full fetch. Acceptable up to ~10k rows; above that
    // consider implementing server-side `orderBy`.
    remote?: boolean
    autoFetch?: boolean
}

export function useDataTable(
    loader: Function,
    filter: Object | Function,
    autoFetchOrOptions: boolean | UseDataTableOptions = true,
) {
    // Back-compat: third arg was a boolean (autoFetch) in older call
    // sites. Detect & normalise.
    const opts: UseDataTableOptions = typeof autoFetchOrOptions === 'boolean'
        ? { autoFetch: autoFetchOrOptions }
        : (autoFetchOrOptions || {})
    const remote = opts.remote !== false  // default true
    const autoFetch = opts.autoFetch !== false

    const state = reactive({
        loading: false,
        data: [] as any[],
    })
    const pagination = reactive({
        page: 1,
        pageCount: 1,
        pageSize: loadPageSize(),
        itemCount: 0,
        showSizePicker: true,
        pageSizes: [10, 20, 50, 100],
        prefix({ itemCount }: any) {
            return t('texts.records', { total: itemCount } as any, itemCount)
        }
    })
    // Current client-side sort. Set by handleSorterChange() from the
    // NDataTable `@update:sorter` event.
    const sorter = ref<SorterState | null>(null)
    // Columns registered by the page via setSortColumns(). Used to look up
    // the column's `sorter` function by `columnKey` when the user clicks a
    // header. Kept out of `state` to avoid making the (usually very wide)
    // column definitions reactive.
    let sortColumns: any[] = []

    function setSortColumns(cols: any[]) {
        sortColumns = cols || []
    }

    function handleSorterChange(s: SorterState | null) {
        // Naive UI emits `null` when the column is unsorted (third click).
        // Store the state and let the computed do the work.
        if (!s || !s.order) {
            sorter.value = null
        } else {
            sorter.value = { columnKey: s.columnKey, order: s.order, sorter: s.sorter }
        }
        // In full-fetch mode, changing the sort resets the view to page 1
        // so the user sees the new top of the dataset (otherwise they'd be
        // looking at "page 3" of the new ordering, which is confusing).
        if (!remote) pagination.page = 1
    }

    // sortedData is the view sorted by the active column's `sorter`
    // callback. In remote mode it sorts only `state.data` (the current
    // page — known limitation). In full-fetch mode it sorts the ENTIRE
    // dataset. Either way the underlying array is never mutated (NDataTable
    // passes the same reference back and would stack sort directions).
    const sortedData = computed(() => {
        const s = sorter.value
        if (!s || !s.order) return state.data
        const col = sortColumns.find((c: any) => c && c.key === s.columnKey)
        const fn: any = col?.sorter
        if (typeof fn !== 'function') return state.data
        const copy = [...state.data]
        copy.sort((a, b) => {
            const r = fn(a, b)
            return s.order === 'ascend' ? r : -r
        })
        return copy
    })

    // paginatedData is what the table actually renders in full-fetch mode.
    // It's `sortedData` sliced to the current page. In remote mode it's
    // just `sortedData` (the backend already returned the page).
    const paginatedData = computed(() => {
        if (remote) return sortedData.value
        const start = (pagination.page - 1) * pagination.pageSize
        return sortedData.value.slice(start, start + pagination.pageSize)
    })

    // Monotonic request generation. Each fetchData captures its own token;
    // responses that arrive out-of-order (e.g. because the user rapidly
    // switched host/filter) are discarded if a newer request has started
    // meanwhile.
    let requestGen = 0
    const fetchData = async function (page: number = 1) {
        const myGen = ++requestGen
        state.data = [];
        state.loading = true;
        try {
            let args = typeof filter === 'function' ? filter() : filter
            args = isRef(args) ? args.value : args
            // Full-fetch mode: request the entire dataset. The server
            // already uses `misc.Page` which maps pageSize=999999 to
            // "return everything" for Docker-backed endpoints. For
            // DAO-backed endpoints (Mongo skip/limit) the limit becomes
            // effectively unbounded. Any future endpoint that returns
            // truly unbounded data should be migrated to remote=true
            // with a server-side `orderBy`.
            const payload = remote
                ? { ...args, pageIndex: page, pageSize: pagination.pageSize }
                : { ...args, pageIndex: 1, pageSize: 999999 }
            let r = await loader(payload);
            if (myGen !== requestGen) return  // stale response, drop it
            const items = r.data?.items || [];
            const total = r.data?.total ?? items.length
            state.data = items;
            if (remote) {
                pagination.itemCount = total
                pagination.page = page
                pagination.pageCount = Math.ceil(total / pagination.pageSize)
            } else {
                // In full-fetch mode the client is the source of truth for
                // pagination — `itemCount` drives the page picker, `page`
                // is reset so a filter change or refetch lands on page 1.
                pagination.itemCount = items.length
                pagination.page = 1
                pagination.pageCount = Math.max(1, Math.ceil(items.length / pagination.pageSize))
            }
        } finally {
            if (myGen === requestGen) state.loading = false;
        }
    }
    const changePage = function (p: number) {
        if (remote) {
            fetchData(p)
        } else {
            pagination.page = p
        }
    }
    const changePageSize = function (size: number) {
        pagination.pageSize = size
        savePageSize(size)
        if (remote) {
            pagination.page = 1
            fetchData()
        } else {
            // No refetch — we already have all the data. Just reset to
            // page 1 and recompute the page count; `paginatedData`
            // reactively slices the new window.
            pagination.page = 1
            pagination.pageCount = Math.max(1, Math.ceil(pagination.itemCount / size))
        }
    }

    if (autoFetch) {
        onMounted(fetchData)
    }

    return {
        state,
        pagination,
        fetchData,
        changePage,
        changePageSize,
        sortedData,
        paginatedData,
        handleSorterChange,
        setSortColumns,
    }
}
