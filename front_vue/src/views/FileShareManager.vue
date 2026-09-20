<template>
  <div>
    <MyHeader />
    <main class="page-container files-page">
      <header class="files-hero">
        <div>
          <span class="page-eyebrow">PRIVATE FILE DELIVERY</span>
          <h1>文件分享</h1>
          <p>文件只保存一份，每条分享链接都能单独设置访问人、口令、到期时间和下载次数。</p>
        </div>
        <div class="hero-stat"><strong>{{ files.length }}</strong><span>个文件</span></div>
      </header>

      <section class="card upload-card" aria-labelledby="upload-title">
        <div class="upload-copy">
          <span class="upload-mark" aria-hidden="true">↑</span>
          <div><h2 id="upload-title">上传新文件</h2><p>单文件最大 50 MiB。上传完成后再创建一个或多个分享链接。</p></div>
        </div>
        <div class="upload-controls">
          <label class="file-picker" :class="{disabled: uploading}">
            <span>{{ pendingFile ? pendingFile.name : '选择文件' }}</span>
            <input ref="fileInput" type="file" aria-label="选择文件" :disabled="uploading" @change="chooseFile">
          </label>
          <el-button type="primary" size="large" :loading="uploading" :disabled="!pendingFile" @click="uploadFile">上传文件</el-button>
        </div>
        <div v-if="pendingFile" class="pending-file">
          <span>{{ formatBytes(pendingFile.size) }}</span><span v-if="uploading">已上传 {{ uploadProgress }}%</span>
        </div>
        <el-progress v-if="uploading" :percentage="uploadProgress" :show-text="false" />
        <el-alert v-if="uploadError" :title="uploadError" type="error" :closable="false" show-icon />
      </section>

      <el-alert v-if="pageError" :title="pageError" type="error" :closable="false" show-icon class="page-alert" />
      <div class="files-workspace">
        <section class="card file-library" aria-label="我的文件">
          <div class="section-heading">
            <div><span class="page-eyebrow">MY FILES</span><h2>我的文件</h2></div>
            <el-button :loading="loadingFiles" circle aria-label="刷新文件列表" @click="loadFiles(true)">↻</el-button>
          </div>
          <div v-loading="loadingFiles && files.length === 0" class="file-list">
            <article v-for="file in files" :key="file.id" class="file-item" :class="{selected: selectedFile?.id === file.id}">
              <button class="file-main" @click="selectFile(file)">
                <span class="file-type" aria-hidden="true">{{ fileExtension(file.name) }}</span>
                <span class="file-copy"><strong>{{ file.name }}</strong><small>{{ formatBytes(file.size_bytes) }} · {{ file.share_count || 0 }} 条分享</small><small>{{ formatDate(file.create_time) }}</small></span>
              </button>
              <div class="file-actions">
                <el-button size="small" @click="selectFile(file)">管理分享</el-button>
                <el-button size="small" type="danger" plain @click="deleteFile(file)">删除</el-button>
              </div>
            </article>
            <div v-if="!loadingFiles && files.length === 0" class="designed-empty">
              <span aria-hidden="true">⇧</span><h3>还没有文件</h3><p>从上方选择一个文件上传，随后就能创建分享链接。</p>
            </div>
          </div>
          <div v-if="filesHasMore" class="load-more"><el-button :loading="loadingFiles" @click="loadFiles(false)">加载更多</el-button></div>
        </section>

        <section class="card share-panel" aria-label="分享记录">
          <template v-if="selectedFile">
            <div class="section-heading share-heading">
              <div><span class="page-eyebrow">SHARE LINKS</span><h2>{{ selectedFile.name }}</h2><p>{{ selectedFile.share_count || shares.length }} 条独立分享记录</p></div>
              <el-button type="primary" @click="beginShare">新建分享</el-button>
            </div>

            <form v-if="creatingShare" class="share-form" @submit.prevent="createShare">
              <div class="form-title"><div><h3>创建分享链接</h3><p>时间和次数限制可以同时生效，任意一项达到后链接失效。</p></div><button type="button" aria-label="关闭新建分享" @click="creatingShare = false">×</button></div>
              <fieldset><legend>谁可以访问</legend>
                <label v-for="option in accessOptions" :key="option.value" class="choice-card"><input v-model="shareForm.access_mode" type="radio" name="file-access" :value="option.value" @change="accessModeChanged"><span><strong>{{ option.label }}</strong><small>{{ option.description }}</small></span></label>
              </fieldset>
              <label v-if="shareForm.access_mode === 'members'" class="form-field"><span>指定成员</span><el-select v-model="shareForm.member_user_ids" multiple filterable placeholder="选择可以下载的账号" class="full-width"><el-option v-for="user in eligibleUsers" :key="user.id" :label="user.display_name || user.user_name" :value="String(user.id)" /></el-select></label>

              <fieldset class="secret-options"><legend>额外口令</legend>
                <label class="choice-card"><input v-model="shareForm.secret_mode" type="radio" name="secret-mode" value="none"><span><strong>无需额外口令</strong><small>满足身份要求后可直接下载</small></span></label>
                <label v-if="shareForm.access_mode === 'public'" class="choice-card"><input v-model="shareForm.secret_mode" type="radio" name="secret-mode" value="code" aria-label="需要分享码" @change="prepareCode"><span><strong>需要 6 位分享码</strong><small>大小写敏感，仅支持 ASCII 字母和数字</small></span></label>
                <label v-else class="choice-card"><input v-model="shareForm.secret_mode" type="radio" name="secret-mode" value="password"><span><strong>需要分享密码</strong><small>登录身份之外再验证 8～72 字节密码</small></span></label>
              </fieldset>
              <div v-if="shareForm.secret_mode === 'code'" class="secret-editor">
                <div class="secret-source"><label><input v-model="shareForm.code_source" type="radio" value="random" aria-label="随机生成" @change="generateCode">随机生成</label><label><input v-model="shareForm.code_source" type="radio" value="custom">指定分享码</label></div>
                <label class="form-field"><span>6 位分享码</span><div class="inline-control"><input v-model="shareForm.secret" aria-label="6 位分享码" maxlength="6" autocomplete="off" :readonly="shareForm.code_source === 'random'"><el-button v-if="shareForm.code_source === 'random'" @click="generateCode">换一个</el-button></div></label>
              </div>
              <label v-if="shareForm.secret_mode === 'password'" class="form-field"><span>分享密码</span><input v-model="shareForm.secret" type="password" aria-label="分享密码" maxlength="72" autocomplete="new-password" placeholder="8～72 个 UTF-8 字节"></label>

              <div class="limit-grid">
                <label class="form-field"><span>有效期</span><select v-model="shareForm.expiration" aria-label="有效期"><option value="never">不限时间</option><option value="1d">1 天</option><option value="7d">7 天</option><option value="30d">30 天</option><option value="custom">自定义</option></select></label>
                <label v-if="shareForm.expiration === 'custom'" class="form-field"><span>到期时间</span><input v-model="shareForm.custom_expiration" type="datetime-local" aria-label="到期时间"></label>
                <label class="form-field"><span>下载次数</span><select v-model="shareForm.download_limit" aria-label="下载次数"><option value="unlimited">不限次数</option><option value="once">仅 1 次</option><option value="custom">自定义 N 次</option></select></label>
                <label v-if="shareForm.download_limit === 'custom'" class="form-field"><span>最多下载次数</span><input v-model.number="shareForm.max_downloads" type="number" min="1" max="1000000" step="1" aria-label="最多下载次数"></label>
              </div>
              <el-alert v-if="shareError" :title="shareError" type="error" :closable="false" show-icon />
              <div class="form-submit"><el-button @click="creatingShare = false">取消</el-button><el-button native-type="submit" type="primary" :loading="savingShare">创建分享链接</el-button></div>
            </form>

            <section v-if="createdShare" class="created-share" aria-live="polite">
              <div><strong>分享链接已创建</strong><p>口令只在这里显示一次，请和链接分开发送。</p></div>
              <code>{{ absoluteShareURL(createdShare.share) }}</code>
              <code v-if="createdShare.secret" class="created-secret">{{ createdShare.secret }}</code>
              <div><el-button size="small" @click="copyText(absoluteShareURL(createdShare.share), '链接已复制')">复制链接</el-button><el-button v-if="createdShare.secret" size="small" @click="copyText(createdShare.secret, '口令已复制')">复制口令</el-button><el-button size="small" text @click="createdShare = null">关闭</el-button></div>
            </section>

            <div v-loading="loadingShares" class="share-list">
              <article v-for="share in shares" :key="share.id" class="share-record">
                <div class="share-record-top"><div class="share-kind"><strong>{{ accessText(share.access_mode) }}</strong><span>{{ secretText(share.secret_mode) }}</span></div><el-tag :type="shareState(share).type">{{ shareState(share).label }}</el-tag></div>
                <div class="share-link"><code>{{ absoluteShareURL(share) }}</code><el-button size="small" text @click="copyText(absoluteShareURL(share), '链接已复制')">复制</el-button></div>
                <dl><div><dt>有效期</dt><dd>{{ share.expires_at ? formatDate(share.expires_at) : '不限时间' }}</dd></div><div><dt>下载次数</dt><dd>{{ share.download_count || 0 }} / {{ share.max_downloads ? share.max_downloads : '不限' }}</dd></div></dl>
                <div class="share-actions"><el-button v-if="shareState(share).key === 'active'" size="small" type="danger" plain @click="revokeShare(share)">撤销</el-button></div>
              </article>
              <div v-if="!loadingShares && shares.length === 0" class="designed-empty compact"><span aria-hidden="true">↗</span><h3>还没有分享链接</h3><p>创建后，每条链接都可以独立撤销。</p></div>
            </div>
            <div v-if="sharesHasMore" class="load-more"><el-button :loading="loadingShares" @click="loadShares(false)">加载更多</el-button></div>
          </template>
          <div v-else class="designed-empty panel-empty"><span aria-hidden="true">⌁</span><h3>选择一个文件</h3><p>在左侧选择文件，查看和管理它的全部分享记录。</p></div>
        </section>
      </div>
    </main>
  </div>
</template>

<script>
import MyHeader from '@/components/MyHeader.vue'

const {filesApi} = require('@/api/files.cjs')
const {expirationValue, fileShareState, randomShareCode, secretError, uploadFileError} = require('@/utils/file_sharing_behavior.cjs')

function emptyShareForm() {
  return {
    access_mode: 'public', member_user_ids: [], secret_mode: 'none', secret: '', code_source: 'random',
    expiration: 'never', custom_expiration: '', download_limit: 'unlimited', max_downloads: 2,
  }
}

export default {
  name: 'FileShareManager', components: {MyHeader},
  data() {
    return {
      files: [], filesCursor: '', filesHasMore: false, loadingFiles: false, pendingFile: null, uploading: false, uploadProgress: 0, uploadError: '', pageError: '',
      selectedFile: null, shares: [], sharesCursor: '', sharesHasMore: false, loadingShares: false, creatingShare: false, savingShare: false, shareError: '', shareForm: emptyShareForm(), createdShare: null,
      eligibleUsers: [], accessOptions: [
        {value: 'public', label: '任何拿到链接的人', description: '无需登录，可选择 6 位分享码'},
        {value: 'authenticated', label: '本站登录用户', description: '有效账号登录后可以下载'},
        {value: 'members', label: '指定成员', description: '只允许选中的本站账号'},
      ],
    }
  },
  async created() {
    if (!this.$store.state.userInfo) {
      this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
      return
    }
    await Promise.all([this.loadFiles(true), this.loadEligibleUsers()])
  },
  methods: {
    async loadFiles(reset) {
      this.loadingFiles = true; this.pageError = ''
      try {
        const data = await filesApi.listFiles({cursor: reset ? '' : this.filesCursor, limit: 20})
        this.files = reset ? data.items || [] : this.files.concat(data.items || [])
        this.filesCursor = data.next_cursor || ''; this.filesHasMore = Boolean(data.has_more)
      } catch (error) { this.handleError(error, '文件列表加载失败') }
      finally { this.loadingFiles = false }
    },
    async loadEligibleUsers() {
      try { const data = await filesApi.listEligibleUsers(); this.eligibleUsers = data.items || [] }
      catch (error) { if (error.status === 401) this.handleError(error); else this.eligibleUsers = [] }
    },
    chooseFile(event) {
      const file = event.target.files && event.target.files[0]
      const error = uploadFileError(file)
      this.pendingFile = error ? null : file; this.uploadError = error
      if (error) event.target.value = ''
    },
    async uploadFile() {
      const error = uploadFileError(this.pendingFile)
      if (error || this.uploading) { this.uploadError = error; return }
      this.uploading = true; this.uploadProgress = 0; this.uploadError = ''
      try {
        const file = await filesApi.uploadFile(this.pendingFile, event => { if (event.total) this.uploadProgress = Math.min(99, Math.round(event.loaded / event.total * 100)) })
        this.uploadProgress = 100; this.files.push(file); this.pendingFile = null
        if (this.$refs.fileInput) this.$refs.fileInput.value = ''
        this.$message.success('文件上传完成')
      } catch (requestError) { this.uploadError = requestError.message || '文件上传失败' }
      finally { this.uploading = false }
    },
    async selectFile(file) {
      this.selectedFile = file; this.createdShare = null; this.creatingShare = false
      await this.loadShares(true)
    },
    async loadShares(reset) {
      if (!this.selectedFile) return
      this.loadingShares = true; this.shareError = ''
      try {
        const data = await filesApi.listShares(this.selectedFile.id, {cursor: reset ? '' : this.sharesCursor, limit: 20})
        this.shares = reset ? data.items || [] : this.shares.concat(data.items || [])
        this.sharesCursor = data.next_cursor || ''; this.sharesHasMore = Boolean(data.has_more)
      } catch (error) { this.handleError(error, '分享记录加载失败') }
      finally { this.loadingShares = false }
    },
    beginShare() {
      this.shareForm = emptyShareForm(); this.createdShare = null; this.shareError = ''; this.creatingShare = true
    },
    accessModeChanged() {
      this.shareForm.member_user_ids = []
      if (this.shareForm.access_mode === 'public' && this.shareForm.secret_mode === 'password') this.shareForm.secret_mode = 'none'
      if (this.shareForm.access_mode !== 'public' && this.shareForm.secret_mode === 'code') this.shareForm.secret_mode = 'none'
    },
    prepareCode() {
      this.shareForm.code_source = 'random'; this.generateCode()
    },
    generateCode() {
      this.shareForm.secret = randomShareCode()
    },
    sharePayload() {
      const secretValidation = secretError(this.shareForm.secret_mode, this.shareForm.secret)
      if (secretValidation) throw new Error(secretValidation)
      if (this.shareForm.access_mode === 'members' && this.shareForm.member_user_ids.length === 0) throw new Error('请至少选择一位成员')
      const maxDownloads = this.shareForm.download_limit === 'unlimited' ? 0 : this.shareForm.download_limit === 'once' ? 1 : Number(this.shareForm.max_downloads)
      if (!Number.isSafeInteger(maxDownloads) || maxDownloads < 0 || maxDownloads > 1000000) throw new Error('下载次数必须是 1～1000000 的整数')
      return {
        access_mode: this.shareForm.access_mode,
        member_user_ids: this.shareForm.access_mode === 'members' ? this.shareForm.member_user_ids.map(String) : [],
        secret_mode: this.shareForm.secret_mode,
        secret: this.shareForm.secret_mode === 'none' ? '' : this.shareForm.secret,
        expires_at: expirationValue(this.shareForm.expiration, this.shareForm.custom_expiration),
        max_downloads: maxDownloads,
      }
    },
    async createShare() {
      if (this.savingShare) return
      let payload
      try { payload = this.sharePayload() } catch (error) { this.shareError = error.message; return }
      this.savingShare = true; this.shareError = ''
      try {
        const result = await filesApi.createShare(this.selectedFile.id, payload)
        this.shares.unshift(result.share); this.selectedFile.share_count = Number(this.selectedFile.share_count || 0) + 1
        this.createdShare = result; this.creatingShare = false
      } catch (error) { this.shareError = error.message || '分享链接创建失败' }
      finally { this.savingShare = false }
    },
    async revokeShare(share) {
      try { await this.$confirm('撤销后链接立即失效，历史下载次数会保留。', '撤销分享链接', {type: 'warning', confirmButtonText: '确认撤销'}) }
      catch (_) { return }
      try {
        const result = await filesApi.revokeShare(this.selectedFile.id, share.id)
        share.revoked_at = result.revoked_at; this.$message.success('分享链接已撤销')
      } catch (error) { this.handleError(error, '撤销失败') }
    },
    async deleteFile(file) {
      try { await this.$confirm(`删除“${file.name}”后，它的全部分享链接都会失效。`, '删除文件', {type: 'warning', confirmButtonText: '确认删除'}) }
      catch (_) { return }
      try {
        await filesApi.deleteFile(file.id); this.files = this.files.filter(item => item.id !== file.id)
        if (this.selectedFile?.id === file.id) { this.selectedFile = null; this.shares = [] }
        this.$message.success('文件已删除')
      } catch (error) { this.handleError(error, '删除失败') }
    },
    async copyText(value, message) {
      try { await navigator.clipboard.writeText(value); this.$message.success(message) }
      catch (_) { this.$message.error('复制失败，请手动复制') }
    },
    handleError(error, fallback = '操作失败') {
      if (error.status === 401) { this.$store.commit('REMOVE_INFO'); this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}}); return }
      this.pageError = error.message || fallback
    },
    shareState(share) { return fileShareState(share) },
    absoluteShareURL(share) { return new URL(share.url || `/s/${share.token}`, window.location.origin).href },
    accessText(mode) { return {public: '任何人', authenticated: '登录用户', members: '指定成员'}[mode] || mode },
    secretText(mode) { return {none: '无额外口令', code: '6 位分享码', password: '分享密码'}[mode] || mode },
    formatDate(value) { const date = new Date(value); return Number.isNaN(date.getTime()) ? '未知时间' : date.toLocaleString('zh-CN', {hour12: false}) },
    formatBytes(bytes) { const value = Number(bytes || 0); return value < 1024 ? `${value} B` : value < 1048576 ? `${(value / 1024).toFixed(1)} KiB` : `${(value / 1048576).toFixed(1)} MiB` },
    fileExtension(name) { const part = String(name || '').split('.').pop(); return part && part !== name ? part.slice(0, 4).toUpperCase() : 'FILE' },
  },
}
</script>

<style scoped>
.files-page{max-width:1220px}.files-hero{position:relative;display:flex;align-items:flex-end;justify-content:space-between;gap:30px;overflow:hidden;margin-bottom:22px;border-radius:22px;background:linear-gradient(125deg,#173f38,#2b685c);padding:34px 38px;color:#fff;box-shadow:0 22px 50px rgba(21,62,53,.13)}.files-hero:after{position:absolute;right:9%;bottom:-70px;width:240px;height:240px;border:1px solid rgba(255,255,255,.12);border-radius:50%;content:""}.files-hero .page-eyebrow{color:#b9d9cd}.files-hero h1{margin:0 0 8px;font-size:clamp(28px,4vw,40px);letter-spacing:-1px}.files-hero p{max-width:700px;margin:0;color:#d5e6df;line-height:1.8}.hero-stat{position:relative;z-index:1;min-width:108px;border:1px solid rgba(255,255,255,.16);border-radius:16px;background:rgba(255,255,255,.08);padding:14px 18px;text-align:center;backdrop-filter:blur(8px)}.hero-stat strong,.hero-stat span{display:block}.hero-stat strong{font-size:28px}.hero-stat span{color:#c4d9d1;font-size:12px}.upload-card{margin-bottom:22px;padding:22px 26px}.upload-copy{display:flex;align-items:center;gap:14px}.upload-copy h2{margin:0;font-size:18px}.upload-copy p{margin:4px 0 0;color:var(--text-secondary);font-size:13px}.upload-mark{display:grid;width:44px;height:44px;flex:0 0 44px;place-items:center;border-radius:13px;background:#eaf4ee;color:var(--primary-dark);font-size:22px}.upload-controls{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:12px;margin-top:18px}.file-picker{display:flex;min-width:0;min-height:44px;align-items:center;overflow:hidden;border:1px dashed #93b7a8;border-radius:10px;background:#f5faf6;color:var(--primary-dark);cursor:pointer;padding:8px 14px}.file-picker span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-picker input{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);clip-path:inset(50%)}.file-picker.disabled{cursor:not-allowed;opacity:.6}.pending-file{display:flex;justify-content:space-between;margin:9px 2px;color:var(--text-secondary);font-size:12px}.page-alert{margin-bottom:20px}.files-workspace{display:grid;grid-template-columns:minmax(330px,.78fr) minmax(0,1.3fr);align-items:start;gap:22px}.file-library,.share-panel{min-width:0;padding:24px}.share-panel{position:sticky;top:98px}.section-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:14px;margin-bottom:18px}.section-heading .page-eyebrow{margin-bottom:5px}.section-heading h2{margin:0;font-size:20px;overflow-wrap:anywhere}.section-heading p{margin:6px 0 0;color:var(--text-secondary);font-size:12px}.file-list,.share-list{display:grid;gap:10px;min-height:100px}.file-item{display:grid;grid-template-columns:minmax(0,1fr) auto;align-items:center;gap:10px;border:1px solid var(--border-color);border-radius:13px;background:#fcfdfc;padding:10px;transition:border-color .18s,background .18s}.file-item.selected{border-color:#8fb6a6;background:#f1f7f3}.file-main{display:flex;min-width:0;align-items:center;gap:11px;border:0;background:transparent;color:inherit;cursor:pointer;padding:3px;text-align:left}.file-type{display:grid;width:42px;height:42px;flex:0 0 42px;place-items:center;border-radius:11px;background:#e9f2ec;color:var(--primary-dark);font-size:10px;font-weight:750}.file-copy{min-width:0}.file-copy strong,.file-copy small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-copy strong{font-size:14px}.file-copy small{color:var(--text-secondary);font-size:11px}.file-actions{display:flex;gap:6px}.file-actions .el-button{margin:0}.designed-empty{display:flex;min-height:230px;align-items:center;justify-content:center;flex-direction:column;border:1px dashed #c9d9d0;border-radius:14px;background:#fafcf9;padding:30px;text-align:center}.designed-empty>span{display:grid;width:54px;height:54px;place-items:center;border-radius:50%;background:#e9f3ed;color:var(--primary-color);font-size:25px}.designed-empty h3{margin:15px 0 4px;font-size:16px}.designed-empty p{max-width:300px;margin:0;color:var(--text-secondary);font-size:12px;line-height:1.7}.designed-empty.compact{min-height:180px}.panel-empty{min-height:400px}.share-form{margin-bottom:18px;border:1px solid #c9dbd1;border-radius:15px;background:#f8fbf8;padding:20px}.form-title{display:flex;justify-content:space-between;gap:12px;margin-bottom:18px}.form-title h3{margin:0;font-size:17px}.form-title p{margin:4px 0 0;color:var(--text-secondary);font-size:12px}.form-title>button{width:32px;height:32px;border:0;border-radius:50%;background:#e8efea;color:#52665d;cursor:pointer;font-size:20px}.share-form fieldset{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin:0 0 16px;border:0;padding:0}.share-form legend{grid-column:1/-1;margin-bottom:7px;font-size:13px;font-weight:700}.choice-card{display:flex;align-items:flex-start;gap:8px;border:1px solid var(--border-color);border-radius:10px;background:#fff;padding:11px}.choice-card:has(input:checked){border-color:#79a895;background:#eef6f1}.choice-card input{width:17px;height:17px;flex:0 0 17px}.choice-card strong,.choice-card small{display:block}.choice-card strong{font-size:12px}.choice-card small{margin-top:3px;color:var(--text-secondary);font-size:10px;line-height:1.45}.form-field{display:block;min-width:0;margin-bottom:14px}.form-field>span{display:block;margin-bottom:6px;color:#53655e;font-size:12px;font-weight:650}.form-field>input,.form-field>select,.inline-control>input{width:100%;min-height:42px;border:1px solid var(--border-color);border-radius:9px;background:#fff;color:var(--text-primary);font:inherit;padding:8px 11px}.secret-source{display:flex;flex-wrap:wrap;gap:16px;margin-bottom:10px;font-size:12px}.secret-source label{display:flex;align-items:center;gap:6px}.inline-control{display:flex;gap:8px}.inline-control>input{min-width:0}.limit-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:0 12px}.form-submit{display:flex;justify-content:flex-end;gap:8px;margin-top:18px}.form-submit .el-button{margin:0}.created-share{display:grid;gap:10px;margin-bottom:16px;border:1px solid #91bba7;border-radius:14px;background:#edf7f1;padding:17px}.created-share strong{color:var(--primary-dark)}.created-share p{margin:3px 0 0;color:var(--text-secondary);font-size:12px}.created-share code,.share-link code{min-width:0;overflow-wrap:anywhere;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:12px}.created-secret{width:max-content;border-radius:7px;background:#fff;padding:7px 10px;font-size:16px!important;letter-spacing:1px}.created-share .el-button{margin:0}.share-record{border:1px solid var(--border-color);border-radius:13px;background:#fff;padding:16px}.share-record-top,.share-link{display:flex;align-items:center;justify-content:space-between;gap:12px}.share-kind strong,.share-kind span{display:block}.share-kind span{margin-top:2px;color:var(--text-secondary);font-size:11px}.share-link{margin:13px 0;border-radius:8px;background:#f3f7f4;padding:7px 9px}.share-link .el-button{flex-shrink:0;margin:0}.share-record dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin:0}.share-record dl>div{border-left:2px solid #d5e4db;padding-left:9px}.share-record dt{color:var(--text-secondary);font-size:10px}.share-record dd{margin:1px 0 0;font-size:12px;overflow-wrap:anywhere}.share-actions{display:flex;justify-content:flex-end;margin-top:12px}.load-more{text-align:center;margin-top:16px}
@media(max-width:900px){.files-workspace{grid-template-columns:1fr}.share-panel{position:static}.file-library,.share-panel{padding:20px}}
@media(max-width:560px){.files-page{padding-left:12px;padding-right:12px}.files-hero{align-items:flex-start;flex-direction:column;padding:27px 22px}.files-hero p{font-size:13px}.hero-stat{display:flex;align-items:center;gap:8px;padding:8px 12px}.hero-stat strong{font-size:20px}.upload-card{padding:18px 15px}.upload-copy{align-items:flex-start}.upload-controls{grid-template-columns:1fr}.upload-controls .el-button{width:100%;margin:0}.file-library,.share-panel{padding:16px 12px}.file-item{grid-template-columns:minmax(0,1fr)}.file-actions{display:grid;grid-template-columns:1fr 1fr}.file-actions .el-button{width:100%}.share-heading{align-items:stretch;flex-direction:column}.share-heading>.el-button{width:100%;margin:0;min-height:44px}.share-form{padding:17px 12px}.share-form fieldset,.limit-grid{grid-template-columns:1fr}.choice-card{min-height:58px}.form-submit{display:grid;grid-template-columns:1fr 1fr}.form-submit .el-button{width:100%}.share-record dl{grid-template-columns:1fr}.created-share>div:last-child{display:grid;grid-template-columns:1fr 1fr;gap:6px}.created-share .el-button{width:100%}.panel-empty{min-height:260px}}
.file-library{position:sticky;top:98px}
@media(max-width:900px){.file-library{position:static}}
</style>
