import {createRouter, createWebHistory} from 'vue-router'

import MyLogin from '../views/Login'
import MyIndex from '../views/Index'
import BookInfo from "@/views/BookInfo"
import BookList from "@/views/BookList"
import AddBook from "@/views/AddBook"
import ScanCodePage from "../views/ScanCodePage"

const routes = [
    {
        path: '/',
        name: 'Index',
        component: MyIndex,
        meta: {
            requireAuth: false
        }
    },
    {
        path: '/login',
        name: 'Login',
        component: MyLogin,
        meta: {
            requireAuth: false
        }
    },
    {
        path: '/book/info',
        name: 'BookInfo',
        component: BookInfo,
        meta: {
            requireAuth: false
        }
    },
    {
        path: '/book/list',
        name: 'BookList',
        component: BookList,
        meta: {
            requireAuth: true
        }
    },
    {
        path: '/book/add',
        name: 'BookAdd',
        component: AddBook,
        meta: {
            requireAuth: true
        }
    },
    {
        title: '扫码页面',
        name: 'scanCodePage',
        path: '/scanCodePage',
        component: ScanCodePage,
        meta: {
            requireAuth: false
        }
    }

]

const router = createRouter({
    history: createWebHistory(),
    base: '/',
    routes: routes,
})

export default router
