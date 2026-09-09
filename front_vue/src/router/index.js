import {createRouter, createWebHistory} from 'vue-router'

import MyLogin from '../views/Login'
import MyIndex from '../views/Index'
import BookInfo from "@/views/BookInfo"
import BookList from "@/views/BookList"
import AddBook from "@/views/AddBook"
import ScanCodePage from "../views/ScanCodePage"
import WebProjectEditor from '@/views/WebProjectEditor'
import WebProjectList from '@/views/WebProjectList'
import WebProjectOpen from '@/views/WebProjectOpen'

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
        path: '/web-projects',
        name: 'WebProjectList',
        component: WebProjectList,
        meta: {
            title: '网页项目',
            requireAuth: true
        }
    },
    {
        path: '/web-projects/new',
        name: 'WebProjectCreate',
        component: WebProjectEditor,
        meta: {
            title: '新建网页项目',
            requireAuth: true
        }
    },
    {
        path: '/web-projects/open',
        name: 'WebProjectOpen',
        component: WebProjectOpen,
        meta: {
            title: '打开网页项目',
            requireAuth: false
        }
    },
    {
        path: '/web-projects/:id',
        name: 'WebProjectDetail',
        component: WebProjectEditor,
        meta: {
            title: '项目设置',
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
