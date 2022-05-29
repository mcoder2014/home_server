import router from "./router";

// 用来控制是否在前端拦截无权限的页面
const isEnable = false

router.beforeEach(async (to, from) => {

    if (!isEnable) {
        return
    }

    const token = localStorage.getItem("token")
    let isAuthenticated = token != null;
    // console.log("token:" + token + "to:" + to + "from:" + from)

    if (to.matched.some(record => record.meta.requireAuth)) {
        if (
            // 检查用户是否已登录
            !isAuthenticated && to.name !== 'Login'
        ) {
            // 将用户重定向到登录页面
            return {name: 'Login'}
        }
    }
})