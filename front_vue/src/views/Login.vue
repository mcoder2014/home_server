<template>
  <div class="login-page">
    <router-link to="/" class="login-brand"><span class="brand-mark" aria-hidden="true">CQ</span>{{ $store.state.site.title }}</router-link>
    <main class="login-layout">
      <section class="login-intro">
        <span class="page-eyebrow">一个属于自己的数字空间</span>
        <h1>生活里的好想法，<br>在这里安放。</h1>
        <p>管理家庭藏书，发布实用网页。<br>用同一个账号，连接你的每一份收藏与创造。</p>
        <div class="intro-note"><span></span>你的内容，由你决定与谁分享</div>
      </section>
      <div class="login-card">
      <div class="login-header">
        <h2 class="login-title">登录你的空间</h2>
        <p class="login-subtitle">欢迎回来，请输入账号信息。</p>
      </div>

      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="form-message" />
      <el-form
        :model="ruleForm"
        :rules="rules"
        ref="ruleForm"
        label-position="top"
        class="login-form"
        @submit.prevent="submitForm('ruleForm')"
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="ruleForm.username"
            placeholder="请输入用户名"
            autocomplete="username"
            :prefix-icon="User"
            size="large"
          />
        </el-form-item>
        <el-form-item label="密码" prop="password">
          <el-input
            type="password"
            v-model="ruleForm.password"
            placeholder="请输入密码"
            autocomplete="current-password"
            :prefix-icon="Lock"
            show-password
            size="large"
          />
        </el-form-item>
        <el-form-item>
          <el-button
            type="primary"
            size="large"
            class="login-btn"
            native-type="submit"
            :loading="loading"
          >
            登录
          </el-button>
        </el-form-item>
      </el-form>
      <router-link v-if="$store.state.registration.enabled" to="/register" class="back-home register-link">有邀请码？注册账号</router-link>
      <p v-else class="closed-registration">网站暂未开放注册，请联系管理员</p>
      <router-link to="/" class="back-home">返回首页</router-link>
      </div>
    </main>
    <footer class="login-footer">{{ $store.state.site.title }} · 留给生活的一点数字空间</footer>
  </div>
</template>

<script>
import {User, Lock} from '@element-plus/icons-vue'
const {accountsApi} = require('@/api/accounts.cjs')
const {isSafeProjectTarget, normalizeInternalRedirect} = require('@/utils/web_projects_navigation.cjs')
export default {
  name: 'MyLogin',
  data() { return {ruleForm: {username: '', password: ''}, loading: false, error: '', rules: {username: [{required: true, message: '请输入用户名', trigger: 'blur'}], password: [{required: true, message: '请输入密码', trigger: 'blur'}]}} },
  setup() { return {User, Lock} },
  async created() {
    if (this.$route.query.registered) this.$message.success('账号已创建，请登录')
    if (this.$route.query.expired) this.error = '登录状态已失效，请重新登录'
    if (this.$route.query.changed) this.$message.success('密码已更新，请使用新密码重新登录')
    try { await this.$store.dispatch('loadBootstrap') } catch (error) { this.error = error.message }
  },
  methods: {
    loginRedirect() {
      const hash = typeof this.$route.hash === 'string' && this.$route.hash.startsWith('#') ? this.$route.hash.slice(1) : ''
      const fragment = new URLSearchParams(hash)
      const target = fragment.has('redirect') ? fragment.get('redirect') : this.$route.query.redirect
      return normalizeInternalRedirect(target)
    },
    async submitForm() {
      if (this.loading || !await this.$refs.ruleForm.validate().catch(() => false)) return
      this.loading = true
      this.error = ''
      try {
        const user = await accountsApi.login({user_name: this.ruleForm.username, password: this.ruleForm.password})
        this.$store.commit('SET_USERINFO', user)
        this.ruleForm.password = ''
        if (user.must_change_password) { await this.$router.replace('/account/security'); return }
        const redirect = this.loginRedirect()
        if (isSafeProjectTarget(redirect)) window.location.replace(redirect)
        else await this.$router.replace(redirect)
      } catch (error) { this.error = error.message || '登录失败，请稍后重试' }
      finally { this.loading = false }
    },
  },
}
</script>
<style scoped>
.form-message {margin-bottom:20px}
.register-link {margin-bottom:18px;color:var(--primary-color)}
.closed-registration {font-size:12px;color:var(--text-secondary);text-align:center;margin-bottom:20px}
.login-page { min-height: 100vh; display: flex; flex-direction: column; padding: 32px 48px 24px; background: radial-gradient(ellipse at 12% 46%, #e3eee2 0%, transparent 58%), #f5f6f1; }
.login-brand { display: inline-flex; gap: 12px; align-items: center; align-self: flex-start; color: var(--text-primary); text-decoration: none; font-size: 17px; font-weight: 700; letter-spacing: -0.4px; }
.login-layout { display: grid; grid-template-columns: 1fr 400px; align-items: center; gap: 80px; max-width: 1000px; width: 100%; flex: 1; margin: 50px auto; }
.login-intro h1 { font-size: clamp(30px, 4vw, 44px); font-weight: 650; letter-spacing: -1px; line-height: 1.5; margin: 18px 0 22px; }
.login-intro p { color: var(--text-secondary); font-size: 15px; line-height: 2; }
.intro-note { display: flex; align-items: center; gap: 10px; font-size: 12px; color: #6c806e; margin-top: 42px; }
.intro-note span { height: 1px; width: 28px; background: #9bb49b; }
.login-card { background: #fff; border: 1px solid #e3e9df; border-radius: 20px; box-shadow: 0 18px 60px #203a2b08; padding: 38px 36px 28px; width: 100%; }
.login-header { margin-bottom: 30px; }
.login-title { margin: 0 0 8px; font-size: 24px; font-weight: 650; }
.login-subtitle { margin: 0; font-size: 13px; color: var(--text-secondary); }
.login-form :deep(.el-form-item) { margin-bottom: 24px; }
.login-form :deep(.el-input__wrapper) { min-height: 46px; }
.login-btn { width: 100%; height: 46px; font-size: 15px; margin-top: 6px; }
.back-home { display: block; text-align: center; color: var(--text-secondary); font-size: 12px; text-decoration: none; }
.back-home:hover { color: var(--primary-color); }
.login-footer { text-align: center; color: #7c8a80; font-size: 11px; }
@media (max-width: 820px) {
  .form-message {margin-bottom:20px}
.register-link {margin-bottom:18px;color:var(--primary-color)}
.closed-registration {font-size:12px;color:var(--text-secondary);text-align:center;margin-bottom:20px}
.login-page { padding: 24px; }
  .login-layout { grid-template-columns: 1fr; max-width: 420px; gap: 26px; margin: 40px auto; }
  .login-intro h1 { font-size: 28px; margin: 12px 0; }
  .login-intro p, .intro-note { display: none; }
  .login-card { padding: 30px 26px; }
}
</style>
