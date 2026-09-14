import router from './router'
import store from './store'
const {routeDecision} = require('@/utils/accounts_behavior.cjs')

router.beforeEach(async to => {
    try {
        // Protected navigation refreshes the real identity, including permissions revoked in another tab.
        if (to.meta.requireAuth || !store.state.sessionLoaded) await store.dispatch('refreshSession')
    } catch (error) {
        if (to.meta.requireAuth) return {path: '/unavailable'}
    }
    return routeDecision(to, store.state.userInfo) || true
})
