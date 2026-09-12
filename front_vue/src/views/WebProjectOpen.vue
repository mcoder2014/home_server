<template>
  <main class="open-page">
    <el-card class="open-card">
      <div v-if="loading" class="open-status">
        <el-icon class="is-loading" :size="32"><Loading /></el-icon>
        <h2>正在建立网页访问登录态</h2>
        <p>完成后会自动打开托管网页。</p>
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
    }
  },
  created() {
    this.openTarget()
  },
  methods: {
    async openTarget() {
      const target = normalizeInternalRedirect(this.$route.query.target, this.$route.hash)
      if (!isSafeProjectTarget(target)) {
        this.loading = false
        this.errorMessage = '目标地址无效，只能打开本站 /p/ 下的托管网页。'
        return
      }

      if (!localStorage.getItem('token')) {
        this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
        return
      }

      try {
        await webShareApi.createBrowserLogin()
      } catch (error) {
        if (error.status === 401) {
          localStorage.removeItem('token')
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
        if (error.status === 401) {
          this.errorMessage = '浏览器未能保存网页访问 Cookie。请允许本站 Cookie，或确认当前页面使用 HTTPS 后重试。'
          return
        }
        this.errorMessage = error.message || '登录已建立，但当前托管网页无法访问。'
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
</style>
