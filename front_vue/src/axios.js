import axios from 'axios'
import { ElMessage, ElMessageBox } from 'element-plus'
import router from './router'
import store from './store'

// 前置拦截
axios.interceptors.request.use(config => {
    if (store.state.token.length > 0) {
        config.headers['passport'] = `${store.state.token}`;
        config.headers.Authorization = `token ${store.state.token}`;
    }
    return config
})

axios.interceptors.response.use(response => {
        let res = response.data;

        console.log("=================")
        console.log(res)
        console.log("=================")

        if (res.code === 200) {
            return response
        } else if (res.code === 301) {
            console.log(response)
        } else {
            ElMessageBox.alert('Error Code: ' + res.code, '这是一条错误信息', {
                confirmButtonText: 'OK',
                callback: (action) => {},
            })

            return Promise.reject(response.data.msg)
        }
    },
    error => {
        console.log(error)
        if (error.response.data) {
            error.message = error.response.data.msg
        }

        if (error.response.status === 401) {
            store.commit("REMOVE_INFO")
            router.push("/login")
        }

        Element.Message.error(error.message, {duration: 3 * 1000})
        return Promise.reject(error)
    }
)