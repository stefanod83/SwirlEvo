import { RouteLocationRaw } from 'vue-router'
import { router } from '@/router/router'

/**
 * Navigate "back" from an Edit / New / Detail page to a sensible context.
 *
 * Goal: fix the UX bug where the Return button on an edit page always dropped
 * the user on the list, even when they had just arrived from the view / detail
 * page of the same entity.
 *
 * Strategy:
 *   1. If the browser history has a previous entry *within the same SPA*, use
 *      `router.back()` — this preserves the exact origin (view, list, deep
 *      link target, etc.) and restores scroll / filter state the browser
 *      kept for us.
 *   2. Otherwise (deep link, fresh tab, direct paste) fall back to the
 *      supplied route — typically the entity's Detail view when editing an
 *      existing record, or the List when creating a new one.
 *
 * Vue Router 4 exposes `window.history.state.back: string | null` which is
 * set for every in-app navigation; `null` means the current entry is the
 * first one in the history stack.
 */
export function returnTo(fallback: RouteLocationRaw): void {
  const state = window.history.state as { back?: string | null } | null
  if (state && state.back) {
    router.back()
    return
  }
  router.push(fallback)
}
