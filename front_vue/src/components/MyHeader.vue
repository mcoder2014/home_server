<template>
  <header class="app-header">
    <div class="header-inner">
      <!-- 左侧 Logo + 应用名 -->
      <div class="header-logo" @click="$router.push('/')">
        <el-icon :size="24" color="#409eff"><Reading /></el-icon>
        <span class="logo-text">图书管理系统</span>
      </div>

      <!-- 中间导航菜单 -->
      <el-menu
        mode="horizontal"
        router
        :ellipsis="false"
        class="header-nav"
      >
        <el-sub-menu index="lib">
          <template #title>图书管理</template>
          <el-menu-item index="/book/list">图书列表</el-menu-item>
          <el-menu-item index="/book/add">录入图书</el-menu-item>
          <el-menu-item index="/book/search">图书检索</el-menu-item>
        </el-sub-menu>
      </el-menu>

      <!-- 右侧用户信息 -->
      <div class="header-user">
        <template v-if="hasLogin">
          <el-avatar
            shape="circle"
            :size="32"
            :src="user.avatar"
          />
          <span class="username">{{ user.username }}</span>
          <el-button type="danger" plain size="small" @click="logout">退出</el-button>
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
import { Reading } from '@element-plus/icons-vue'

export default {
  name: "MyHeader",
  components: { Reading },
  data() {
    return {
      user: {
        username: '请先登录',
        avatar: 'https://cube.elemecdn.com/3/7c/3ea6beec64369c2642b92c6726f1epng.png'
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

      apiBase.post("/passport/logout").then(function (response) {
        console.log(response);
        if (response.data.code === 0) {
          console.log(response)
        } else {
          alert("login failed")
        }

        // 最终都需要退出
        localStorage.removeItem('token')
        localStorage.removeItem('user_name')
        curRouter.push({ path: '/' });
      }).catch(function (err) {
        alert("error " + err)
      })
    }
  },
  created() {
    if (localStorage.getItem('token') !== null && localStorage.getItem('token') !== '') {
      this.hasLogin = true
      this.user.username = localStorage.getItem('user_name')
    }
  }
}
</script>

<style scoped>
.app-header {
  position: sticky;
  top: 0;
  z-index: 100;
  background: #ffffff;
  box-shadow: 0 1px 4px rgba(0, 0, 0, 0.12);
  height: var(--header-height);
}

.header-inner {
  display: flex;
  align-items: center;
  height: 100%;
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 16px;
}

.header-logo {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  flex-shrink: 0;
}

.logo-text {
  font-size: 16px;
  font-weight: 700;
  color: var(--text-primary);
  white-space: nowrap;
}

.header-nav {
  flex: 1;
  border-bottom: none;
  background: transparent;
  margin-left: 24px;
}

.header-nav :deep(.el-menu--horizontal) {
  border-bottom: none;
}

.header-user {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
  margin-left: 16px;
}

.username {
  font-size: 14px;
  color: var(--text-secondary);
  white-space: nowrap;
}
</style>
