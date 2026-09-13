import {createApp} from 'vue'
import router from './router'
import store from './store'

import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'
import './assets/global.css'

import "./permission"
import App from "./App"

window.addEventListener('account-session-expired', () => {
    const route = router.currentRoute.value
    if (route.meta.requireAuth) router.replace({path: '/login', query: {expired: '1', redirect: route.fullPath}})
})

router.afterEach((to, from, failure) => {
    if (!failure) document.title = `${store.state.site.title} · ${to.meta.title || '首页'}`
})
store.subscribe(mutation => {
    if (mutation.type === 'SET_BOOTSTRAP') document.title = `${store.state.site.title} · ${router.currentRoute.value.meta.title || '首页'}`
})
store.dispatch('loadBootstrap').catch(() => {})

const app = createApp(App)
// Make sure to _use_ the router instance to make the
// whole app router-aware.
app.use(router).use(store).use(ElementPlus)

app.mount('#app')

