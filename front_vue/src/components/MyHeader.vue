<template>
  <header class="app-header">
    <div class="header-inner">
      <router-link class="header-logo" to="/" aria-label="CQ Home Server 首页">
        <span class="brand-mark" aria-hidden="true">CQ</span>
        <span class="logo-text">CQ Home Server</span>
      </router-link>

      <nav class="header-nav" aria-label="主导航">
        <router-link to="/" :class="{active: $route.path === '/'}">首页</router-link>
        <router-link to="/web-share" :class="{active: $route.path.startsWith('/web-share') || $route.path.startsWith('/web-projects')}">
          <el-icon><Monitor /></el-icon>网页托管
        </router-link>
        <router-link to="/book/list" :class="{active: $route.path.startsWith('/book/')}">
          <el-icon><Reading /></el-icon>图书管理
        </router-link>
        <router-link to="/applications" :class="{active: $route.path === '/applications'}">
          <el-icon><Key /></el-icon>应用凭证
        </router-link>
      </nav>

      <!-- 右侧用户信息 -->
      <div class="header-user">
        <template v-if="hasLogin">
          <span class="user-avatar" aria-hidden="true">{{ user.username.slice(0, 1).toUpperCase() }}</span>
          <span class="username" :title="user.username">{{ user.username }}</span>
          <el-button plain size="small" @click="logout">退出</el-button>
        </template>
        <template v-else>
          <el-button type="primary" plain size="small" @click="$router.push('/login')">登录</el-button>
        </template>
      </div>
    </div>
  </header>
</template>


<script>
import axios from "axios";
import {Key, Monitor, Reading} from '@element-plus/icons-vue'

export default {
  name: "MyHeader",
  components: {Key, Monitor, Reading},
  data() {
    return {
      user: {
        username: '请先登录'
      },
      hasLogin: false
    }
  },
  methods: {
    logout() {
      let url = this.$store.state.global.baseUrl + "/"
      let apiBase = axios.create({
        baseURL: url,
        withCredentials: false,
        headers: {'passport': localStorage.getItem('token')}
      });

      let curRouter = this.$router

      apiBase.post("/passport/logout").then((response) => {
        if (response.data.code !== 0) {
          alert("退出失败，请稍后重试")
          return
        }

        // 服务端确认 token 失效后再清理本地身份，保持内容 Cookie 与界面状态一致。
        localStorage.removeItem('token')
        localStorage.removeItem('user_name')
        this.hasLogin = false
        curRouter.push({ path: '/' });
      }).catch(function (err) {
        alert("error " + err)
      })
    }
  },
  created() {
    if (localStorage.getItem('token') !== null && localStorage.getItem('token') !== '') {
      this.hasLogin = true
      this.user.username = localStorage.getItem('user_name') || '用户'
    }
  }
}
</script>

<style scoped>
.app-header {
  position: sticky;
  top: 0;
  z-index: 100;
  background: rgba(255, 255, 255, 0.96);
  border-bottom: 1px solid var(--border-color);
}
.header-inner {
  display: flex;
  align-items: center;
  min-height: var(--header-height);
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 28px;
  gap: 32px;
}
.header-logo { display: flex; align-items: center; gap: 10px; text-decoration: none; flex-shrink: 0; }
.logo-text { font-size: 16px; font-weight: 700; color: var(--text-primary); white-space: nowrap; letter-spacing: -0.4px; }
.header-nav { display: flex; align-items: center; gap: 6px; flex: 1; }
.header-nav a { display: flex; align-items: center; justify-content: center; gap: 7px; min-height: 40px; padding: 0 13px; border-radius: 8px; font-size: 14px; color: var(--text-secondary); text-decoration: none; white-space: nowrap; transition: background 0.18s; }
.header-nav a:hover { color: var(--primary-color); background: #f5f8f5; }
.header-nav a.active { color: var(--primary-dark); background: #edf4ef; font-weight: 600; }
.header-user { display: flex; align-items: center; gap: 9px; flex-shrink: 0; }
.user-avatar { display: grid; place-items: center; width: 30px; height: 30px; border-radius: 50%; background: #edf0e7; color: #687447; font-size: 12px; font-weight: 650; }
.username { font-size: 13px; color: var(--text-secondary); max-width: 100px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 820px) {
  .header-inner { display: grid; grid-template-columns: 1fr auto; gap: 0 12px; padding: 14px 20px 10px; }
  .header-nav { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); grid-row: 2; grid-column: 1 / -1; margin-top: 12px; width: 100%; }
  .header-nav a { min-width: 0; padding-left: 6px; padding-right: 6px; }
  .header-user { grid-column: 2; grid-row: 1; }
}
@media (max-width: 480px) {
  .header-inner { padding-left: 16px; padding-right: 16px; }
  .logo-text { font-size: 15px; }
  .username { display: none; }
  .user-avatar { width: 26px; height: 26px; }
  .header-user { gap: 6px; }
  .header-nav { gap: 2px; }
  .header-nav a { font-size: 12px; gap: 0; padding-left: 3px; padding-right: 3px; }
  .header-nav a .el-icon { display: none; }
}
</style>
