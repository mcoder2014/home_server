import {createStore} from 'vuex'
import {config} from '@/global'
const {accountsApi} = require('@/api/accounts.cjs')
const {configureBrowserSession} = require('@/api/browser_client.cjs')
const {startAccountUpdates} = require('@/utils/account_updates.cjs')

// Remove browser credentials from the former login flow; identity is restored from the HttpOnly cookie.
for (const key of ['token', 'user_name']) localStorage.removeItem(key)
sessionStorage.removeItem('userInfo')
let pendingSession = null
let pendingSessionEpoch = -1
let accountUpdates = null

const store = createStore({
    state: {
        global: {baseUrl: config.serverUrl},
        userInfo: null,
        sessionLoaded: false,
        sessionEpoch: 0,
        site: {title: 'CQ Home Server', notice: ''},
        registration: {enabled: false, monthly_limit: 3, ttl_days: 7},
        modules: {manuals: false},
        isbn: '',
    },
    mutations: {
        SET_USERINFO(state, user) {
            state.sessionEpoch++
            state.userInfo = user || null
            state.sessionLoaded = true
        },
        SET_ISBN(state, isbn) {
            state.isbn = isbn
            localStorage.setItem('isbn', isbn)
        },
        SET_BOOTSTRAP(state, value) {
            state.site = value.site || state.site
            state.registration = value.registration || state.registration
            state.modules = value.modules || state.modules
        },
        REMOVE_INFO(state) {
            state.sessionEpoch++
            state.userInfo = null
            state.sessionLoaded = true
            state.isbn = ''
        },
    },
    getters: {getUser: state => state.userInfo},
    actions: {
        publishProfileUpdate() { if (accountUpdates) accountUpdates.publish() },
        async refreshSession({commit, state}) {
            if (!pendingSession || pendingSessionEpoch !== state.sessionEpoch) {
                const epoch = state.sessionEpoch
                const request = accountsApi.me().then(user => {
                    if (epoch === state.sessionEpoch) commit('SET_USERINFO', user)
                    return state.userInfo
                }).catch(error => {
                    if (epoch !== state.sessionEpoch) return state.userInfo
                    if (error.status === 401) { commit('REMOVE_INFO'); return null }
                    throw error
                }).finally(() => { if (pendingSession === request) pendingSession = null })
                pendingSession = request
                pendingSessionEpoch = epoch
            }
            return pendingSession
        },
        async loadBootstrap({commit}) {
            const value = await accountsApi.bootstrap()
            commit('SET_BOOTSTRAP', value)
            return value
        },
    },
})
configureBrowserSession(() => store.state.userInfo && store.state.userInfo.csrf_token || '', () => { store.commit('REMOVE_INFO'); window.dispatchEvent(new Event('account-session-expired')) })
if (typeof window !== 'undefined') accountUpdates = startAccountUpdates(window, () => store.dispatch('refreshSession'))
export default store
