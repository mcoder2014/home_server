import {createStore} from 'vuex'
import '../global'
import {config} from "@/global";

const store = createStore({
    state: {
        global: {
            // 后端服务域名前缀
            baseUrl: config.serverUrl
        },

        // 用户登录 token
        token: '',
        // 用户信息
        userInfo: JSON.parse(sessionStorage.getItem("userInfo")),
        // 扫码所得图书编码
        isbn: '',

    },
    mutations: {
        // set
        SET_TOKEN: (state, token) => {
            state.token = token
            localStorage.setItem("token", token)
        },
        SET_USERINFO: (state, userInfo) => {
            state.userInfo = userInfo
            sessionStorage.setItem("userInfo", JSON.stringify(userInfo))
        },
        SET_ISBN: (state, isbn) => {
            state.isbn = isbn
            localStorage.setItem("isbn", isbn)
        },
        REMOVE_INFO: (state) => {
            state.token = ''
            state.userInfo = {}
            state.isbn = ''
            localStorage.setItem("token", '')
            sessionStorage.setItem("userInfo", JSON.stringify(''))
        }

    },
    getters: {
        // get
        getUser: state => {
            return state.userInfo
        }

    },
    actions: {},
    modules: {}
})

export default store