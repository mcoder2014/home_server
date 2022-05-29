import {createApp} from 'vue'
import router from './router'
import store from './store'

import ElementPlus from 'element-plus'
import 'element-plus/dist/index.css'

import "./permission"
import App from "./App"

const app = createApp(App)
// Make sure to _use_ the router instance to make the
// whole app router-aware.
app.use(router).use(store).use(ElementPlus)

app.mount('#app')

