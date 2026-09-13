import {createRouter, createWebHistory} from 'vue-router'

import MyLogin from '../views/Login'
import MyIndex from '../views/Index'
import BookInfo from "@/views/BookInfo"
import BookList from "@/views/BookList"
import AddBook from "@/views/AddBook"
import Applications from '@/views/Applications'
import ScanCodePage from "../views/ScanCodePage"
import WebShareEditor from '@/views/WebProjectEditor'
import WebShareList from '@/views/WebProjectList'
import WebShareOpen from '@/views/WebProjectOpen'

const routes = [
    {
        path: '/',
        name: 'Index',
        component: MyIndex,
        meta: {
            title: '首页',
            requireAuth: false
        }
    },
    {
        path: '/login',
        name: 'Login',
        component: MyLogin,
        meta: {
            title: '登录',
            requireAuth: false
        }
    },
    {
        path: '/book/info',
        name: 'BookInfo',
        component: BookInfo,
        meta: {
            title: '图书详情',
            requireAuth: false
        }
    },
    {
        path: '/book/list',
        name: 'BookList',
        component: BookList,
        meta: {
            title: '图书列表',
            requireAuth: true
        }
    },
    {
        path: '/book/add',
        name: 'BookAdd',
        component: AddBook,
        meta: {
            title: '录入图书',
            requireAuth: true
        }
    },
    {
        name: 'scanCodePage',
        path: '/scanCodePage',
        component: ScanCodePage,
        meta: {
            title: '扫码录入',
            requireAuth: false
        }
    },
    {
        path: '/applications',
        name: 'Applications',
        component: Applications,
        meta: {
            title: '应用凭证',
            requireAuth: true
        }
    },
    {
        path: '/web-share',
        alias: '/web-projects',
        name: 'WebShareList',
        component: WebShareList,
        meta: {
            title: '网页托管',
            requireAuth: true
        }
    },
    {
        path: '/web-share/new',
        alias: '/web-projects/new',
        name: 'WebShareCreate',
        component: WebShareEditor,
        meta: {
            title: '新建网页托管',
            requireAuth: true
        }
    },
    {
        path: '/web-share/open',
        alias: '/web-projects/open',
        name: 'WebShareOpen',
        component: WebShareOpen,
        meta: {
            title: '打开托管网页',
            requireAuth: false
        }
    },
    {
        path: '/web-share/:id',
        alias: '/web-projects/:id',
        name: 'WebShareDetail',
        component: WebShareEditor,
        meta: {
            title: '托管设置',
            requireAuth: true
        }
    }

]

const router = createRouter({
    history: createWebHistory(),
    base: '/',
    routes: routes,
})

router.afterEach((to, from, failure) => {
    if (!failure) {
        document.title = `CQ Home Server · ${to.meta.title || '首页'}`
    }
})

export default router
