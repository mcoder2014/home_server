<template>
  <main class="open-page">
    <el-card class="open-card">
      <div v-if="loading" class="open-status">
        <el-icon class="is-loading" :size="32"><Loading /></el-icon>
        <h2>正在建立网页访问登录态</h2>
        <p>完成后会自动打开托管网页。</p>
      </div>
      <div v-else-if="passwordRequired" class="open-status password-status">
        <span class="open-lock" aria-hidden="true">⌁</span>
        <h2>输入阅读密码</h2>
        <p>密码由网页所有者提供，不是你的账号密码。</p>
        <el-input v-model="password" type="password" show-password size="large" maxlength="72" autocomplete="off" aria-label="网页阅读密码" @keyup.enter="unlockProject" />
        <el-alert v-if="errorMessage" :title="errorMessage" type="error" :closable="false" show-icon />
        <el-button type="primary" size="large" class="unlock-button" :loading="unlocking" @click="unlockProject">解锁并打开</el-button>
      </div>
      <el-result v-else icon="error" title="无法打开托管网页" :sub-title="errorMessage">
        <template #extra>
          <el-button type="primary" @click="$router.replace('/web-share')">返回托管列表</el-button>
        </template>
      </el-result>
    </el-card>
  </main>
</template>

<script>
import {Loading} from '@element-plus/icons-vue'

const {webShareApi} = require('@/api/web_projects.cjs')
const {isSafeProjectTarget, normalizeInternalRedirect} = require('@/utils/web_projects_navigation.cjs')

export default {
  name: 'WebShareOpen',
  components: {Loading},
  data() {
    return {
      loading: true,
      errorMessage: '',
      passwordRequired: false,
      password: '',
      unlocking: false,
      target: '',
    }
  },
  created() {
    this.openTarget()
  },
  methods: {
    // 校验托管目标路径和现有浏览器会话，再用 HEAD 确认资源可访问后跳转；登录失效时转到登录页。
    async openTarget() {
      const target = normalizeInternalRedirect(this.$route.query.target, this.$route.hash)
      if (!isSafeProjectTarget(target)) {
        this.loading = false
        this.errorMessage = '目标地址无效，只能打开本站 /p/ 下的托管网页。'
        return
      }
      this.target = target

      if (!this.$store.state.userInfo) {
        this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
        return
      }

      try {
        await webShareApi.checkBrowserSession()
      } catch (error) {
        if (error.status === 401) {
          this.$store.commit('REMOVE_INFO')
          this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
          return
        }
        this.loading = false
        this.errorMessage = error.message || '登录衔接失败，请返回列表后重试。'
        return
      }

      try {
        await webShareApi.probeProjectSession(target)
        window.location.replace(target)
      } catch (error) {
        this.loading = false
        if (error.status === 403 && /^\d+$/.test(String(this.$route.query.project || ''))) {
          this.passwordRequired = true
          this.errorMessage = ''
          return
        }
        if (error.status === 401) {
          this.errorMessage = '浏览器未能保存网页访问 Cookie。请允许本站 Cookie，或确认当前页面使用 HTTPS 后重试。'
          return
        }
        this.errorMessage = error.message || '登录已建立，但当前托管网页无法访问。'
      }
    },
    async unlockProject() {
      const projectID = String(this.$route.query.project || '')
      if (this.unlocking || !this.password) {
        if (!this.password) this.errorMessage = '请输入网页阅读密码'
        return
      }
      if (!/^\d+$/.test(projectID) || !isSafeProjectTarget(this.target)) {
        this.passwordRequired = false
        this.errorMessage = '目标网页信息无效，请返回托管列表后重试。'
        return
      }
      this.unlocking = true
      this.errorMessage = ''
      try {
        await webShareApi.unlockProject(projectID, this.password)
        await webShareApi.probeProjectSession(this.target)
        window.location.replace(this.target)
      } catch (error) {
        if (error.status === 401) {
          this.$store.commit('REMOVE_INFO')
          this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
          return
        }
        this.errorMessage = error.status === 429 ? '尝试过于频繁，请稍后再试' : error.status === 403 ? '阅读密码不正确，请重新输入' : error.message || '网页解锁失败'
      } finally {
        this.unlocking = false
      }
    },
  },
}
</script>

<style scoped>
.open-page {
  align-items: center;
  display: flex;
  justify-content: center;
  min-height: 100vh;
  padding: 24px;
}

.open-card {
  max-width: 520px;
  width: 100%;
}

.open-status {
  padding: 32px 20px;
  text-align: center;
}

.open-status h2 {
  font-size: 20px;
  margin: 18px 0 8px;
}

.open-status p {
  color: var(--text-secondary);
  margin: 0;
}
.password-status{display:grid;gap:15px}.password-status h2,.password-status p{margin:0}.open-lock{display:grid;width:54px;height:54px;margin:0 auto;place-items:center;border-radius:16px;background:#e7f2ec;color:var(--primary-dark);font-size:24px}.unlock-button{width:100%;min-height:46px;margin:0}
</style>
