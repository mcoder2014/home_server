<template>
  <MyHeader />
  <main class="page-container">
    <el-result :icon="unavailable ? 'warning' : 'info'" :title="title" :sub-title="description">
      <template #extra><el-button type="primary" @click="$router.push('/')">返回首页</el-button><el-button v-if="unavailable" @click="retry">重新连接</el-button></template>
    </el-result>
  </main>
</template>
<script>
import MyHeader from '@/components/MyHeader'
export default {
  name: 'AccessState', components: {MyHeader},
  computed: {
    unavailable() { return this.$route.path === '/unavailable' },
    title() { const feature = {library: '家庭藏书', applications: '应用凭证', web_projects: '网页托管'}[this.$route.query.feature]; return this.unavailable ? '暂时无法确认登录状态' : this.$route.query.reason === 'disabled' ? `${feature || '此功能'}已由管理员关闭` : feature ? `${feature}未开通` : '仅管理员可以使用此功能' },
    description() { return this.unavailable ? '服务连接失败，登录身份尚未核实，请稍后重试。' : '请联系管理员开通权限；已有页面入口不会授予操作权限。' },
  },
  methods: {async retry() { try { await this.$store.dispatch('refreshSession'); this.$router.replace('/') } catch (error) { this.$message.error(error.message) } }},
}
</script>
