<template>
  <main class="receive-page">
    <div class="receive-glow glow-one"></div><div class="receive-glow glow-two"></div>
    <router-link to="/" class="receive-brand"><span class="brand-mark" aria-hidden="true">CQ</span><span>{{ $store.state.site.title }}</span></router-link>
    <section class="receive-card" aria-live="polite">
      <div v-if="loading" class="receive-loading"><span class="file-seal loading-seal">⌁</span><h1>正在检查分享链接</h1><p>请稍候，页面不会自动开始下载。</p></div>
      <div v-else-if="unavailable" class="receive-state"><span class="file-seal unavailable-seal">×</span><span class="page-eyebrow">LINK UNAVAILABLE</span><h1>这个分享链接不可用</h1><p>{{ error || '链接可能已过期、被撤销，或下载次数已经用完。' }}</p><router-link class="home-link" to="/">返回首页</router-link></div>
      <template v-else-if="share">
        <header class="receive-heading"><span class="file-seal">↓</span><div><span class="page-eyebrow">FILE DELIVERY</span><h1>{{ share.file ? share.file.name : '受保护文件' }}</h1><p>{{ share.file ? `${formatBytes(share.file.size_bytes)} · ` : '' }}{{ accessText(share.access_mode) }}</p></div></header>
        <dl v-if="share.state === 'available'" class="share-facts"><div><dt>有效期</dt><dd>{{ share.expires_at ? formatDate(share.expires_at) : '不限时间' }}</dd></div><div><dt>剩余次数</dt><dd>{{ share.remaining_downloads == null ? '不限次数' : `${share.remaining_downloads} 次` }}</dd></div></dl>

        <form v-if="share.state === 'locked'" class="unlock-form" @submit.prevent="unlock">
          <div class="lock-note"><strong>需要{{ share.secret_mode === 'code' ? '分享码' : '分享密码' }}</strong><p>口令由分享者提供，不是你的账号密码。</p></div>
          <label><span>{{ share.secret_mode === 'code' ? '6 位分享码' : '分享密码' }}</span><el-input v-model="secret" :aria-label="share.secret_mode === 'code' ? '6 位分享码' : '分享密码'" :maxlength="share.secret_mode === 'code' ? 6 : 72" :show-password="share.secret_mode !== 'code'" type="password" autocomplete="off" size="large" /></label>
          <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
          <el-button native-type="submit" type="primary" size="large" :loading="unlocking" class="primary-action">解锁文件</el-button>
        </form>
        <div v-else-if="share.state === 'login_required'" class="login-required"><strong>登录后才能下载</strong><p>登录成功后会回到当前分享页，下载不会自动开始。</p><el-button type="primary" size="large" class="primary-action" @click="goLogin">前往登录</el-button></div>
        <div v-else class="download-ready">
          <div class="ready-note"><span>✓</span><div><strong>文件已准备好</strong><p>点击后会占用 1 次下载次数；传输中断也不会返还。</p></div></div>
          <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
          <el-button type="primary" size="large" class="primary-action" :loading="downloading" :disabled="downloading" @click="download">下载文件</el-button>
        </div>
      </template>
    </section>
    <p class="receive-footnote">由 {{ $store.state.site.title }} 传递 · 请妥善保管分享链接与口令</p>
  </main>
</template>

<script>
const {filesApi} = require('@/api/files.cjs')
const {parseDownloadName} = require('@/utils/file_sharing_behavior.cjs')

export default {
  name: 'FileShareReceive',
  data() { return {share: null, loading: true, unavailable: false, secret: '', unlocking: false, downloading: false, error: ''} },
  created() { this.loadShare() },
  methods: {
    async loadShare() {
      const token = String(this.$route.params.token || '')
      if (!/^[A-Za-z0-9_-]{32,128}$/.test(token)) { this.loading = false; this.unavailable = true; return }
      this.loading = true; this.error = ''
      try { this.share = await filesApi.getShare(token); this.unavailable = false }
      catch (error) {
        if (error.status === 401) this.share = {state: 'login_required', access_mode: 'authenticated'}
        else { this.unavailable = true; this.error = error.status === 404 ? '' : error.message || '分享链接暂时无法访问' }
      } finally { this.loading = false }
    },
    async unlock() {
      if (this.unlocking || !this.secret) { if (!this.secret) this.error = '请输入分享口令'; return }
      this.unlocking = true; this.error = ''
      try { await filesApi.unlockShare(this.$route.params.token, this.secret); this.secret = ''; await this.loadShare() }
      catch (error) {
        if (error.status === 401) { this.share.state = 'login_required'; this.error = '' }
        else if (error.status === 429) this.error = '尝试过于频繁，请稍后再试'
        else this.error = error.status === 403 ? '口令不正确，请重新输入' : error.message || '解锁失败'
      } finally { this.unlocking = false }
    },
    async download() {
      if (this.downloading) return
      this.downloading = true; this.error = ''
      try {
        const response = await filesApi.downloadShare(this.$route.params.token)
        const header = response.headers && (response.headers['content-disposition'] || response.headers.get && response.headers.get('content-disposition'))
        const filename = parseDownloadName(header, this.share.file && this.share.file.name || 'download')
        const url = URL.createObjectURL(response.data)
        const link = document.createElement('a'); link.href = url; link.download = filename; document.body.appendChild(link); link.click(); link.remove()
        setTimeout(() => URL.revokeObjectURL(url), 1000)
        this.share.download_count = Number(this.share.download_count || 0) + 1
        if (this.share.remaining_downloads != null) this.share.remaining_downloads = Math.max(0, Number(this.share.remaining_downloads) - 1)
      } catch (error) {
        if (error.status === 403) { this.share.state = 'locked'; this.error = '分享口令已失效，请重新解锁' }
        else if (error.status === 401) { this.share.state = 'login_required'; this.error = '' }
        else if (error.status === 404) { this.unavailable = true; this.error = '' }
        else this.error = error.message || '下载失败，请稍后重试'
      } finally { this.downloading = false }
    },
    goLogin() { this.$router.push({path: '/login', query: {redirect: this.$route.fullPath}}) },
    accessText(mode) { return {public: '公开分享', authenticated: '登录用户分享', members: '指定成员分享'}[mode] || '文件分享' },
    formatDate(value) { const date = new Date(value); return Number.isNaN(date.getTime()) ? '未知时间' : date.toLocaleString('zh-CN', {hour12: false}) },
    formatBytes(bytes) { const value = Number(bytes || 0); return value < 1024 ? `${value} B` : value < 1048576 ? `${(value / 1024).toFixed(1)} KiB` : `${(value / 1048576).toFixed(1)} MiB` },
  },
}
</script>

<style scoped>
.receive-page{position:relative;display:flex;min-height:100vh;align-items:center;justify-content:center;overflow:hidden;background:linear-gradient(145deg,#edf4ef 0%,#f8f7f1 48%,#e9f0ec 100%);padding:88px 20px 56px}.receive-brand{position:absolute;top:28px;left:34px;display:flex;align-items:center;gap:11px;color:var(--text-primary);font-weight:700;text-decoration:none}.receive-card{position:relative;z-index:1;width:min(100%,570px);overflow:hidden;border:1px solid rgba(144,174,159,.48);border-radius:26px;background:rgba(255,255,255,.92);box-shadow:0 32px 90px rgba(28,68,57,.13);padding:36px;backdrop-filter:blur(14px)}.receive-heading{display:flex;align-items:center;gap:18px;padding-bottom:25px;border-bottom:1px solid var(--border-color)}.file-seal{display:grid;width:64px;height:64px;flex:0 0 64px;place-items:center;border-radius:18px;background:#1f554a;color:#fff;font-size:27px;box-shadow:0 10px 24px rgba(31,85,74,.18)}.receive-heading .page-eyebrow{margin:0 0 5px}.receive-heading h1{margin:0;font-size:clamp(23px,5vw,31px);line-height:1.25;overflow-wrap:anywhere}.receive-heading p{margin:7px 0 0;color:var(--text-secondary);font-size:13px}.share-facts{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px;margin:22px 0}.share-facts>div{border-radius:12px;background:#f2f6f3;padding:13px 15px}.share-facts dt{color:var(--text-secondary);font-size:11px}.share-facts dd{margin:2px 0 0;font-size:13px;font-weight:650;overflow-wrap:anywhere}.unlock-form,.download-ready,.login-required{display:grid;gap:16px;margin-top:24px}.lock-note strong,.login-required strong{font-size:17px}.lock-note p,.ready-note p,.login-required p{margin:4px 0 0;color:var(--text-secondary);font-size:12px;line-height:1.7}.unlock-form label>span{display:block;margin-bottom:7px;color:var(--text-secondary);font-size:12px;font-weight:650}.primary-action{width:100%;min-height:48px;margin:0;font-size:15px}.ready-note{display:flex;align-items:center;gap:12px;border-radius:12px;background:#edf7f1;padding:14px}.ready-note>span{display:grid;width:30px;height:30px;flex:0 0 30px;place-items:center;border-radius:50%;background:#2a785f;color:#fff}.receive-state,.receive-loading{text-align:center;padding:20px 10px}.receive-state .file-seal,.receive-loading .file-seal{margin:0 auto 22px}.receive-state h1,.receive-loading h1{margin:7px 0 9px;font-size:27px}.receive-state p,.receive-loading p{margin:0 auto 22px;max-width:390px;color:var(--text-secondary);font-size:13px;line-height:1.8}.unavailable-seal{background:#746b61}.loading-seal{animation:pulse 1.5s ease-in-out infinite;background:#55786d}.home-link{display:inline-flex;border-bottom:1px solid #8caf9f;padding:3px;color:var(--primary-dark);text-decoration:none}.receive-footnote{position:absolute;bottom:22px;left:20px;right:20px;margin:0;color:#6d8179;font-size:11px;text-align:center}.receive-glow{position:absolute;border-radius:50%;filter:blur(2px)}.glow-one{top:-140px;right:-80px;width:420px;height:420px;background:radial-gradient(circle,rgba(76,137,115,.18),transparent 68%)}.glow-two{bottom:-180px;left:-120px;width:480px;height:480px;border:1px solid rgba(45,102,84,.13)}@keyframes pulse{50%{transform:scale(.94);opacity:.72}}
@media(max-width:520px){.receive-page{align-items:flex-start;padding:84px 12px 60px}.receive-brand{top:20px;left:18px}.receive-brand .brand-mark{width:34px;height:34px}.receive-card{border-radius:20px;padding:25px 18px}.receive-heading{align-items:flex-start;gap:13px}.file-seal{width:50px;height:50px;flex-basis:50px;border-radius:14px;font-size:22px}.share-facts{grid-template-columns:1fr;gap:8px}.receive-state,.receive-loading{padding:12px 0}.receive-state h1,.receive-loading h1{font-size:23px}}
</style>
