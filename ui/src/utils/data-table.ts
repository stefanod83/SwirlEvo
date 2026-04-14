import { isRef, onMounted, reactive } from "vue"
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

export function useDataTable(loader: Function, filter: Object | Function, autoFetch: boolean = true) {
    const state = reactive({
        loading: false,
        data: [],
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
            let r = await loader({
                ...args,
                pageIndex: page,
                pageSize: pagination.pageSize,
            });
            if (myGen !== requestGen) return  // stale response, drop it
            state.data = r.data?.items || [];
            pagination.itemCount = r.data?.total || 0
            pagination.page = page
            pagination.pageCount = Math.ceil(pagination.itemCount / pagination.pageSize)
        } finally {
            if (myGen === requestGen) state.loading = false;
        }
    }
    const changePageSize = function (size: number) {
        pagination.page = 1
        pagination.pageSize = size
        savePageSize(size)
        fetchData()
    }

    if (autoFetch) {
        onMounted(fetchData)
    }

    return { state, pagination, fetchData, changePageSize }
}
