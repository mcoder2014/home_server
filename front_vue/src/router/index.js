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
import ManualList from '@/views/ManualList'
import ManualEditor from '@/views/ManualEditor'
import ManualDetail from '@/views/ManualDetail'
import FileShareManager from '@/views/FileShareManager'
import FileShareReceive from '@/views/FileShareReceive'

const routes = [
    {path: '/register', name: 'Register', component: () => import('@/views/Register.vue'), meta: {title: '受邀注册'}},
    {path: '/account', name: 'Account', component: () => import('@/views/Account.vue'), meta: {title: '个人中心', requireAuth: true}},
    {path: '/account/security', name: 'AccountSecurity', component: () => import('@/views/Account.vue'), meta: {title: '账户安全', requireAuth: true}},
    {path: '/account/sessions', name: 'AccountSessions', component: () => import('@/views/Account.vue'), meta: {title: '登录管理', requireAuth: true}},
    {path: '/invitations', name: 'Invitations', component: () => import('@/views/Invitations.vue'), meta: {title: '邀请朋友', requireAuth: true}},
    {path: '/forbidden', name: 'Forbidden', component: () => import('@/views/AccessState.vue'), meta: {title: '功能未开通'}},
    {path: '/unavailable', name: 'Unavailable', component: () => import('@/views/AccessState.vue'), meta: {title: '服务暂不可用'}},
    {path: '/admin', component: () => import('@/views/AdminLayout.vue'), meta: {requireAuth: true, admin: true}, children: [
        {path: '', redirect: '/admin/users'},
        {path: 'users', component: () => import('@/views/AdminUsers.vue'), meta: {title: '用户管理'}},
        {path: 'web-share', component: () => import('@/views/AdminProjects.vue'), meta: {title: '网页审核'}},
        {path: 'config', component: () => import('@/views/AdminConfig.vue'), meta: {title: '站点设置'}},
        {path: 'audit-logs', component: () => import('@/views/AdminAudit.vue'), meta: {title: '操作记录'}},
    ]},
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
            library: true,
            requireAuth: true
        }
    },
    {
        path: '/book/list',
        name: 'BookList',
        component: BookList,
        meta: {
            title: '图书列表',
            library: true,
            requireAuth: true
        }
    },
    {
        path: '/book/add',
        name: 'BookAdd',
        component: AddBook,
        meta: {
            title: '录入图书',
            library: true,
            requireAuth: true
        }
    },
    {
        name: 'scanCodePage',
        path: '/scanCodePage',
        component: ScanCodePage,
        meta: {
            title: '扫码录入',
            library: true,
            requireAuth: true
        }
    },
    {
        path: '/files',
        name: 'FileShareManager',
        component: FileShareManager,
        meta: {title: '文件分享', requireAuth: true, module: 'file_sharing'}
    },
    {
        path: '/s/:token',
        name: 'FileShareReceive',
        component: FileShareReceive,
        meta: {title: '接收文件', requireAuth: false}
    },
    {
        path: '/applications',
        name: 'Applications',
        component: Applications,
        meta: {
            title: '应用凭证',
            capability: 'applications',
            requireAuth: true
        }
    },
    {
        path: '/manuals',
        name: 'ManualList',
        component: ManualList,
        meta: {title: '家庭说明书', requireAuth: false}
    },
    {
        path: '/manuals/new',
        name: 'ManualCreate',
        component: ManualEditor,
        meta: {title: '新建说明书', capability: 'manuals', requireAuth: true}
    },
    {
        path: '/manuals/:id/edit',
        name: 'ManualEdit',
        component: ManualEditor,
        meta: {title: '编辑说明书', capability: 'manuals', requireAuth: true}
    },
    {
        path: '/manuals/:id',
        name: 'ManualDetail',
        component: ManualDetail,
        meta: {title: '说明书详情', requireAuth: false}
    },
    {
        path: '/web-share',
        alias: '/web-projects',
        name: 'WebShareList',
        component: WebShareList,
        meta: {
            title: '网页托管',
            capability: 'web_projects',
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
            capability: 'web_projects',
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
            capability: 'web_projects',
            requireAuth: true
        }
    },
    {
        path: '/web-share/:id',
        alias: '/web-projects/:id',
        name: 'WebShareDetail',
        component: WebShareEditor,
        meta: {
            title: '托管设置',
            capability: 'web_projects',
            requireAuth: true
        }
    }

]

const router = createRouter({
    history: createWebHistory(),
    base: '/',
    routes: routes,
})

export default router
