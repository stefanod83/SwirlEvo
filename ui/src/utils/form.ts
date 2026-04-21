import { Result } from "@/api/ajax";
import { Ref, ref } from "vue"
import { FormItemRule } from "naive-ui";
import { t } from "@/locales";

export function useForm<T>(form: Ref, action: () => Promise<Result<T>>, success?: (data: T) => void) {
    const submiting = ref(false)
    async function submit(e: Event) {
        e.preventDefault();
        form.value.validate(async (errors: any) => {
            if (errors) {
                return
            }

            submiting.value = true;
            try {
                let r = await action()
                success ? success(<T>r.data) : window.message.info(t('texts.action_success'));
            } finally {
                submiting.value = false;
            }
        });
    }

    return { submit, submiting }
}

export function requiredRule(field?: string, message?: string): FormItemRule {
    return {
        required: true,
        message: formatMessage(field, message ?? t('tips.required_rule')),
        trigger: ["input", "blur"],
    }
}

export function customRule(validator: (rule: any, value: any) => boolean, message?: string, field?: string, required?: boolean): FormItemRule {
    return createRule(validator, message, field, required)
}


export function emailRule(field?: string): FormItemRule {
    const reg = /^([a-zA-Z0-9]+[-_\.]?)+@[a-zA-Z0-9]+\.[a-z]+$/;
    return regexRule(reg, t('tips.email_rule'), field)
}

export function phoneRule(field?: string): FormItemRule {
    const reg = /^[1][3,4,5,7,8][0-9]{9}$/;
    return regexRule(reg, t('tips.phone_rule'), field)
}


export function lengthRule(min: number, max: number, field?: string): FormItemRule {
    return createRule((rule: any, value: string): boolean => {
        return value.length >= min && value.length <= max
    }, t('tips.length_rule', { min, max }), field)
}

export function passwordRule(field?: string): FormItemRule {
    const reg = /^[a-zA-Z0-9_-]+$/;
    return regexRule(reg, t('tips.password_rule'), field)
}

export function regexRule(reg: RegExp, message?: string, field?: string): FormItemRule {
    return {
        message: formatMessage(field, message),
        trigger: ["input", "blur"],
        validator(rule: any, value: string): boolean {
            return !value || reg.test(value)
        },
    };
}

function createRule(validator: (rule: any, value: string) => boolean, message?: string, field?: string, required?: boolean): FormItemRule {
    return {
        required: required,
        message: formatMessage(field, message),
        trigger: ["input", "blur"],
        validator,
    };
}

function formatMessage(field?: string, message?: string) {
    return field ? `${field}: ${message}` : message
}

// urlRule validates http(s):// URLs. Pass `{ scheme: 'https' }` to
// require TLS (used for Keycloak issuer / Vault address); default
// accepts both http and https. Use `schemes` for a custom allowlist
// (e.g. LDAP: ['ldap', 'ldaps']). Empty string is always accepted
// so the rule composes with requiredRule for "optional format".
export function urlRule(opts?: { scheme?: string; schemes?: string[] }, field?: string): FormItemRule {
    const schemes = opts?.schemes ?? (opts?.scheme ? [opts.scheme] : ['http', 'https']);
    const pattern = new RegExp('^(' + schemes.join('|') + ')://[^\\s]+$', 'i');
    return regexRule(pattern, t('validation.invalid_url', { schemes: schemes.join(', ') }), field)
}

// endpointRule validates Docker endpoint schemes: {tcp, unix, ssh}
// plus https for swarm federation. Does NOT accept raw hostnames —
// the backend returns an EndpointSuggestionError for those and the
// handleSaveError helper pops a dialog with "apply and retry".
export function endpointRule(field?: string): FormItemRule {
    const pattern = /^(tcp|unix|ssh|http|https):\/\/[^\s]+$/i;
    return regexRule(pattern, t('validation.invalid_endpoint'), field)
}

// ipRule validates IPv4 addresses. Empty value accepted (compose
// with requiredRule when needed).
export function ipRule(field?: string): FormItemRule {
    const pattern = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/;
    return regexRule(pattern, t('validation.invalid_ip'), field)
}

// cidrRule validates IPv4 CIDR notation (10.0.0.0/16).
export function cidrRule(field?: string): FormItemRule {
    const pattern = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\/([0-9]|[12][0-9]|3[0-2])$/;
    return regexRule(pattern, t('validation.invalid_cidr'), field)
}

// durationRule validates Go/Docker duration strings: 30s, 1m, 2h30m,
// etc. Units allowed: ns, us, µs, ms, s, m, h. Must have at least
// one unit — a bare number is rejected.
export function durationRule(field?: string): FormItemRule {
    const pattern = /^(\d+(ns|us|µs|ms|s|m|h))+$/;
    return regexRule(pattern, t('validation.invalid_duration'), field)
}

// hostnameRule — RFC 1123 hostname. Used by the service Hostname
// override field.
export function hostnameRule(field?: string): FormItemRule {
    const pattern = /^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$/i;
    return regexRule(pattern, t('validation.invalid_hostname'), field)
}

// passwordConfirmRule — cross-field equality. Pass a getter that
// returns the live password value (so Vue reactivity captures
// typing in the primary field). Empty value accepted so the rule
// composes with requiredRule for "optional confirmation".
export function passwordConfirmRule(passwordGetter: () => string, field?: string): FormItemRule {
    return {
        message: formatMessage(field, t('validation.password_mismatch')),
        trigger: ["input", "blur"],
        validator(_: any, value: string): boolean {
            return !value || value === passwordGetter();
        },
    };
}

// handleSaveError inspects a caught axios error and, if the backend
// returned an EndpointSuggestionError envelope, pops a confirm
// dialog offering the suggested endpoint — on accept, mutates
// `model.endpoint` and calls `retry`. Returns true when the envelope
// was handled; caller should not double-display a toast in that case.
// For any other error, returns false so the caller can show its own
// message.
export function handleSaveError(e: any, retry: () => void, dialog: any, model: any): boolean {
    const data = e?.response?.data?.data;
    if (data?.endpointSuggestion && data.suggestedEndpoint) {
        dialog.warning({
            title: t('host_errors.scheme_missing_title'),
            content: t('host_errors.scheme_missing_body', {
                original: data.originalEndpoint,
                suggested: data.suggestedEndpoint,
            }),
            positiveText: t('host_errors.apply_and_retry'),
            negativeText: t('host_errors.cancel'),
            onPositiveClick: () => {
                model.endpoint = data.suggestedEndpoint;
                retry();
            },
        });
        return true;
    }
    if (data?.workerRejected && Array.isArray(data.suggestedManagers) && data.suggestedManagers.length > 0) {
        // Let the existing worker-rejection alert in the page handle
        // this — fall through so the caller can decide how to render
        // the list of manager addresses.
        return false;
    }
    return false;
}