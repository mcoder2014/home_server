<template>
  <header class="app-header"><div class="header-inner">
    <router-link class="header-logo" to="/" aria-label="网站首页"><span class="brand-mark" aria-hidden="true">CQ</span><span class="logo-text">{{ $store.state.site.title }}</span></router-link>
    <nav class="header-nav" aria-label="主导航" v-if="!user?.must_change_password">
      <router-link to="/" :class="{active: $route.path === '/'}">首页</router-link>
      <router-link v-if="filesAvailable" to="/files" :class="{active: $route.path === '/files'}">文件分享</router-link>
      <router-link to="/web-share" :class="{active: /^\/(web-share|web-projects)/.test($route.path)}">网页托管</router-link>
      <router-link v-if="manualsAvailable" to="/manuals" :class="{active: $route.path.startsWith('/manuals')}">说明书管理</router-link>
      <router-link v-if="user?.library_enabled && user?.capabilities?.library !== false" to="/book/list" :class="{active: $route.path.startsWith('/book/')}">家庭藏书</router-link>
      <router-link to="/applications" :class="{active: $route.path === '/applications'}">应用凭证</router-link>
      <router-link v-if="user?.role === 'admin'" to="/admin/users" :class="{active: $route.path.startsWith('/admin')}">管理中心</router-link>
    </nav>
    <div class="header-user">
      <template v-if="user">
        <el-dropdown trigger="click" @command="navigate"><button class="account-trigger"><UserIdentity :user="user" :hide-avatar="user.must_change_password" :size="30" /><span aria-hidden="true">⌄</span></button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="/account/security" v-if="user.must_change_password">修改初始密码</el-dropdown-item><template v-else><el-dropdown-item command="/account">个人中心</el-dropdown-item><el-dropdown-item command="/invitations">邀请朋友</el-dropdown-item></template></el-dropdown-menu></template>
        </el-dropdown>
        <el-button plain size="small" :loading="loggingOut" @click="logout">退出</el-button>
      </template>
      <el-button v-else type="primary" plain size="small" @click="$router.push('/login')">登录</el-button>
    </div>
  </div></header>
</template>
<script>
import UserIdentity from '@/components/UserIdentity.vue'
const {accountsApi} = require('@/api/accounts.cjs')
export default {
  name: 'MyHeader', components: {UserIdentity},
  data() { return {loggingOut: false} },
  computed: {
    user() { return this.$store.state.userInfo },
    filesAvailable() { return this.$store.state.modules?.file_sharing === true },
    manualsAvailable() { return this.$store.state.modules?.manuals === true && (!this.user || this.user.capabilities?.manuals !== false) },
    displayName() { return this.user?.display_name || this.user?.user_name || '用户' },
  },
  methods: {
    navigate(path) { this.$router.push(path) },
    async logout() {
      this.loggingOut = true
      try {
        await accountsApi.logout()
        this.$store.commit('REMOVE_INFO')
        await this.$router.push('/')
      } catch (error) {
        if (error.status === 401) { this.$store.commit('REMOVE_INFO'); await this.$router.push('/login') }
        else this.$message.error(error.message || '退出失败，请稍后重试')
      } finally { this.loggingOut = false }
    },
  },
}
</script>
<style scoped>
.account-trigger {display:flex;align-items:center;gap:8px;border:0;background:none;cursor:pointer;padding:4px;color:var(--text-secondary)}
.app-header {
  position: sticky;
  top: 0;
  z-index: 100;
  background: rgba(250, 252, 250, 0.88);
  border-bottom: 1px solid var(--border-color);
  backdrop-filter: blur(16px) saturate(130%);
}
.header-inner {
  display: flex;
  align-items: center;
  min-height: var(--header-height);
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 28px;
  gap: 20px;
}
.header-logo { display: flex; align-items: center; gap: 10px; text-decoration: none; flex-shrink: 0; }
.logo-text { font-size: 16px; font-weight: 700; color: var(--text-primary); white-space: nowrap; letter-spacing: -0.4px; }
.header-nav { display: flex; align-items: center; gap: 6px; flex: 1; }
.header-nav a { display: flex; align-items: center; justify-content: center; gap: 7px; min-height: 40px; padding: 0 13px; border-radius: 8px; font-size: 14px; color: var(--text-secondary); text-decoration: none; white-space: nowrap; transition: background 0.18s; }
.header-nav a:hover { color: var(--primary-color); background: #f5f8f5; }
.header-nav a.active { color: var(--primary-dark); background: #edf4ef; font-weight: 600; }
.header-user { display: flex; align-items: center; gap: 9px; flex-shrink: 0; }
.user-avatar { display: grid; place-items: center; width: 30px; height: 30px; border-radius: 50%; background: #edf0e7; color: #687447; font-size: 12px; font-weight: 650; }
.account-trigger :deep(.identity-name) { font-size: 13px; color: var(--text-secondary); max-width: 100px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 820px) {
  .header-inner { display: grid; grid-template-columns: 1fr auto; gap: 0 12px; padding: 14px 20px 10px; }
  .header-nav { display: flex; grid-row: 2; grid-column: 1 / -1; width: calc(100% + 40px); margin: 11px -20px 0; padding: 0 20px 3px; overflow-x: auto; scrollbar-width: none; }
  .header-nav::-webkit-scrollbar { display: none; }
  .header-nav a { min-width: max-content; padding-left: 11px; padding-right: 11px; }
  .header-user { grid-column: 2; grid-row: 1; }
}
@media (max-width: 480px) {
  .header-inner { padding-left: 16px; padding-right: 16px; }
  .logo-text { font-size: 15px; }
  .account-trigger :deep(.identity-text) { display: none; }
  .user-avatar { width: 26px; height: 26px; }
  .header-user { gap: 6px; }
  .header-nav { width: calc(100% + 32px); margin-left: -16px; margin-right: -16px; padding-left: 16px; padding-right: 16px; gap: 3px; }
  .header-nav a { font-size: 12px; gap: 0; padding-left: 10px; padding-right: 10px; }
  .header-nav a .el-icon { display: none; }
}
</style>
