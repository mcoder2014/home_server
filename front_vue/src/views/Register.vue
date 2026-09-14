<template>
  <MyHeader />
  <main class="page-container account-narrow">
    <div class="page-eyebrow">欢迎来到我们的数字空间</div><h1 class="page-title">受邀注册</h1>
    <p class="muted">每个邀请码仅可使用一次，自生成起 7 天内有效。</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="form-message" />
    <el-skeleton v-if="loading" :rows="5" animated />
    <el-result v-else-if="!policy.enabled" icon="info" title="网站暂未开放注册" sub-title="请联系管理员；已有邀请码在关闭后永久失效。" />
    <el-form v-else class="surface-card account-form" label-position="top" @submit.prevent="register">
      <el-form-item label="邀请码"><div class="inline-field"><el-input v-model="form.invitation_code" autocomplete="off" @input="invitation = null" placeholder="输入朋友提供的邀请码" /><el-button :loading="validating" :disabled="!form.invitation_code || submitting" @click="validateCode">校验</el-button></div></el-form-item>
      <el-alert v-if="invitation?.valid" type="success" :closable="false" :title="`邀请有效，到期时间：${dateTime(invitation.expires_at)}（UTC+8）`" class="form-message" />
      <el-form-item label="用户名"><el-input v-model="form.user_name" autocomplete="username" maxlength="32" placeholder="3–32 位字母、数字、下划线或短横线" /><small class="muted">以字母或数字开头。用户名用于登录，注册后不能自行修改。</small></el-form-item>
      <el-form-item label="密码"><el-input v-model="form.password" type="password" show-password autocomplete="new-password" :placeholder="`至少 ${minimum} 个字符，可使用空格`" /></el-form-item>
      <el-form-item label="确认密码"><el-input v-model="form.confirm_password" type="password" show-password autocomplete="new-password" /></el-form-item>
      <el-checkbox v-model="agreed">我已了解并同意站点使用与内容审核规则</el-checkbox>
      <p class="muted policy-note">请仅上传有权分享的内容。网页可见范围限制普通访问者；管理员可因站点安全和内容治理查看、下架或删除网页。</p>
      <el-button type="primary" native-type="submit" :loading="submitting" :disabled="!agreed || !invitation?.valid || validating">创建账号</el-button>
      <router-link to="/login" class="text-link">已有账号，去登录</router-link>
    </el-form>
  </main>
</template>
<script>
import MyHeader from '@/components/MyHeader'
const {accountsApi} = require('@/api/accounts.cjs')
const {passwordError, dateTime} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'RegisterPage', components: {MyHeader},
  data() { return {loading: true, submitting: false, validating: false, error: '', policy: {enabled: false}, invitation: null, agreed: false, form: {invitation_code: '', user_name: '', password: '', confirm_password: ''}} },
  computed: {minimum() { return this.policy.min_password_length || 15 }},
  async created() {
    const code = new URLSearchParams(this.$route.hash.replace(/^#/, '')).get('invite')
    if (code) { this.form.invitation_code = code; await this.$router.replace({path: '/register', query: this.$route.query}) }
    try { this.policy = await accountsApi.registrationPolicy(); if (code && this.policy.enabled) await this.validateCode() }
    catch (error) { this.error = error.message }
    finally { this.loading = false }
  },
  beforeUnmount() { this.form.password = ''; this.form.confirm_password = ''; this.form.invitation_code = '' },
  methods: {
    dateTime,
    async validateCode() {
      this.error = ''; this.validating = true; this.invitation = null
      const code = this.form.invitation_code
      try {
        const result = await accountsApi.validateInvitation(code)
        if (code !== this.form.invitation_code) return
        this.invitation = result
        if (!result.valid) this.error = '邀请码不可用，请联系邀请人'
      } catch (error) { this.error = error.message || '邀请码不可用，请联系邀请人' }
      finally { this.validating = false }
    },
    async register() {
      if (this.submitting || !this.agreed || !this.invitation?.valid) return
      this.error = /^[a-zA-Z0-9][a-zA-Z0-9_-]{2,31}$/.test(this.form.user_name) ? passwordError(this.form.password, this.form.confirm_password, this.minimum) : '用户名需要 3–32 位字母、数字、下划线或短横线'
      if (this.error) return
      this.submitting = true
      try { await accountsApi.register({...this.form}); this.form.password = ''; this.form.confirm_password = ''; await this.$router.replace({path: '/login', query: {registered: '1'}}) }
      catch (error) {
        this.error = error.message
        try { this.policy = await accountsApi.registrationPolicy() } catch (_) { /* The original submission error remains visible. */ }
      } finally { this.submitting = false }
    },
  },
}
</script>
