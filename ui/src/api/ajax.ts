import axios, { AxiosInstance, AxiosRequestConfig, AxiosResponse } from 'axios'
import { store } from "../store";
import { router } from "../router/router";
import { Mutations } from "@/store/mutations";
import { t, te } from '@/locales';

// export interface AjaxOptions {
// }

export interface Result<T> {
    code: number;
    info?: string;
    data?: T;
}

class Ajax {
    private ajax: AxiosInstance;

    constructor() {
        this.ajax = axios.create({
            baseURL: import.meta.env.MODE === 'development' ? '/api' : '/api',
            timeout: 30000,
            // withCredentials: true,            
        })

        this.ajax.interceptors.request.use(
            (config: any) => {
                if (store.state.user?.token) {
                    config.headers.Authorization = "Bearer " + store.state.user.token
                }
                // store.commit(Mutations.SetAjaxLoading, true);
                return config;
            },
            (error: any) => {
                return Promise.reject(error);
            }
        )

        this.ajax.interceptors.response.use(
            (response: any) => {
                // store.commit(Mutations.SetAjaxLoading, false);
                return response;
            },
            (error: any) => {
                const handled = this.handleError(error)
                if (handled === 'silence') {
                    // Deploy in progress — return a synthetic empty
                    // success so the caller's `await` resolves cleanly
                    // (no hanging promise = no leaked closures).
                    return Promise.resolve({ data: { code: -1, info: 'self-deploy-in-progress' } })
                }
                if (handled === true) {
                    // Keep the legacy "stop the chain" semantics for
                    // 401/403/404 navigations where the router has
                    // already taken over; the component will unmount
                    // and GC will release the hanging closure shortly.
                    return new Promise(() => { })
                }
                return Promise.reject(error)
            }
        )
    }

    private handleError(error: any): boolean | 'silence' {
        if (error.response) {
            // During an active self-deploy the old Swirl container is
            // being swapped out; transient 401/403/404/500 responses
            // from the reverse proxy are expected. Do NOT redirect to
            // login or blow up the page — the progress modal + polling
            // on /api/system/mode will recover on its own.
            if (store.state.selfDeployInProgress) {
                return 'silence'
            }
            switch (error.response.status) {
                case 401:
                    store.commit(Mutations.Logout);
                    if (error.config.method === "get") {
                        router.replace({
                            name: 'login',
                            query: {
                                redirect: router.currentRoute.value.fullPath
                            }
                        });
                    } else {
                        this.showError(error)
                    }
                    return true
                case 403:
                    router.replace("/403");
                    return true
                case 404:
                    router.replace("/404");
                    return true
                case 500:
                    this.showError(error)
            }
        } else {
            if (store.state.selfDeployInProgress) {
                // Network error during deploy — sidekick is restarting
                // the primary; swallow so the progress modal handles it.
                return 'silence'
            }
            window.message.error(error.message, { duration: 5000 });
        }
        return false
    }

    private showError(error: any) {
        const code = error.response.data?.code || 1;
        const info = te('errors.'+code) ? t('errors.'+code) : error.response.data?.info || error.message;
        window.message.error(info, { duration: 5000 });
    }

    async get<T>(url: string, args?: any, config?: AxiosRequestConfig): Promise<Result<T>> {
        config = { ...config, params: args }
        const r = await this.ajax.get<Result<T>>(url, config);
        return r.data;
    }

    async post<T>(url: string, data?: any, config?: AxiosRequestConfig): Promise<Result<T>> {
        config = { ...config, headers: { 'Content-Type': 'application/json' } }
        // Object.assign(config || {}, {
        //     headers: {
        //         'Content-Type': 'application/json',
        //     },
        // })
        const r = await this.ajax.post<Result<T>>(url, data, config);
        return r.data;
    }

    async request<T>(config: AxiosRequestConfig): Promise<Result<T>> {
        const r = await this.ajax.request<Result<T>>(config);
        return r.data;
    }
}

export default new Ajax;