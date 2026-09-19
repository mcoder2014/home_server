<template>
  <div>
    <MyHeader />
    <main class="page-container manual-editor-page">
      <div v-if="loading" class="editor-loading" v-loading="true">正在加载说明书……</div>
      <el-result v-else-if="fatalError" icon="error" title="无法编辑说明书" :sub-title="fatalError">
        <template #extra><el-button type="primary" @click="$router.push('/manuals')">返回列表</el-button></template>
      </el-result>
      <template v-else>
        <header class="editor-heading">
          <div><span class="page-eyebrow">MANUAL EDITOR</span><h1>{{ manual ? `编辑·${manual.name}` : '新建说明书' }}</h1><p>资料会按下方顺序保存；新建时全部成功后才对外发布。</p></div>
          <router-link v-if="manual" :to="`/manuals/${manual.id}`" class="detail-link">查看说明书</router-link>
        </header>

        <el-alert v-if="localFilesLost" title="页面刷新后无法恢复尚未上传的本地文件，请重新选择这些文件。" type="warning" :closable="false" show-icon class="editor-alert" />
        <el-alert v-if="manual?.status === 'draft'" title="当前是仅你可见的草稿；所有待处理项成功后才会发布。" type="info" :closable="false" show-icon class="editor-alert" />
        <el-alert v-if="notice" :title="notice" :type="noticeType" :closable="false" show-icon class="editor-alert" />

        <section v-if="conflictLatest" class="card conflict-card" aria-label="说明书版本冲突">
          <h2>服务器上已有新版本</h2>
          <p>已读取最新修订 {{ conflictLatest.revision }}。本地表单和上传队列仍保留，未自动覆盖新版本。</p>
          <dl><dt>最新名称</dt><dd>{{ conflictLatest.name }}</dd><dt>最新资料数</dt><dd>{{ conflictLatest.items?.length || 0 }}</dd></dl>
          <el-button type="primary" :disabled="saving || uploading" @click="continueFromLatest">以最新版本继续，保留本地表单</el-button>
        </section>

        <section v-if="manual" class="card editor-section manual-password-section">
          <div class="section-title"><div><h2>阅读密码</h2><p>密码独立于可见范围；所有者仍可直接查看和管理。</p></div><el-tag :type="passwordState.password_protected ? 'success' : 'info'">{{ passwordState.password_protected ? '已启用阅读密码' : '尚未设置阅读密码' }}</el-tag></div>
          <div class="password-grid">
            <label><span>新的说明书阅读密码</span><el-input v-model="passwordDraft" type="password" show-password maxlength="72" autocomplete="new-password" aria-label="新的说明书阅读密码" placeholder="8～72 个 UTF-8 字节" /></label>
            <label><span>确认说明书阅读密码</span><el-input v-model="passwordConfirmation" type="password" show-password maxlength="72" autocomplete="new-password" aria-label="确认说明书阅读密码" @keyup.enter="saveManualPassword" /></label>
          </div>
          <el-alert v-if="passwordNotice" :title="passwordNotice" :type="passwordNoticeType" :closable="false" show-icon class="password-alert" />
          <div class="section-actions password-actions"><el-button type="primary" :loading="passwordSaving" @click="saveManualPassword">{{ passwordState.password_protected ? '更新阅读密码' : '设置阅读密码' }}</el-button><el-button v-if="passwordState.password_protected" :loading="passwordSaving" @click="clearManualPassword">清除阅读密码</el-button></div>
        </section>

        <section class="card editor-section">
          <h2>基本信息</h2>
          <div class="metadata-grid">
            <label class="wide-field"><span>说明书名称</span><input v-model="form.name" aria-label="说明书名称" maxlength="120" placeholder="例如：客厅空调" :disabled="saving || uploading"></label>
            <label class="category-field"><span>分类（最多 20 个）</span><el-select v-model="form.categories" aria-label="分类" multiple filterable allow-create default-first-option :multiple-limit="20" placeholder="选择或输入新分类" :disabled="saving || uploading"><el-option v-for="category in categories" :key="category" :label="category" :value="category" /></el-select></label>
            <label class="wide-field"><span>说明</span><textarea v-model="form.description" aria-label="说明" maxlength="2000" rows="3" placeholder="记录型号、安装位置或注意事项" :disabled="saving || uploading"></textarea></label>
          </div>
          <fieldset class="access-field" :disabled="saving || uploading"><legend>可见范围</legend><label v-for="option in accessOptions" :key="option.value"><input v-model="form.access_mode" type="radio" name="access-mode" :value="option.value"><span><strong>{{ option.label }}</strong><small>{{ option.description }}</small></span></label></fieldset>
          <div v-if="manual" class="section-actions"><el-button :loading="saving" :disabled="saving || uploading" @click="saveSettings">保存设置与顺序</el-button></div>
        </section>

        <section class="card editor-section">
          <div class="section-title"><div><h2>添加资料</h2><p>可多次选择，每次会追加到当前队列。重名文件只提示，不会自动删除。</p></div><span>{{ totalItemCount }} / 100</span></div>
          <div class="source-actions">
            <label class="file-picker" :class="{disabled: saving || uploading}">选择图片<input type="file" aria-label="选择图片" accept="image/jpeg,image/png,image/gif,image/webp,.jpg,.jpeg,.png,.gif,.webp,.heic,.heif" multiple :disabled="saving || uploading" @change="appendFiles"></label>
            <label class="file-picker" :class="{disabled: saving || uploading}">选择 PDF / TXT<input type="file" aria-label="选择 PDF 或 TXT" accept="application/pdf,text/plain,.pdf,.txt" multiple :disabled="saving || uploading" @change="appendFiles"></label>
            <el-button :disabled="saving || uploading" @click="appendStructured('text')">添加文本</el-button>
            <el-button :disabled="saving || uploading" @click="appendStructured('url')">添加网址</el-button>
          </div>
          <p class="format-note">单文件最大 50 MiB；图片支持 JPG、PNG、GIF、WebP。HEIC/HEIF 请先转换为 JPEG 或 PNG。</p>

          <div class="upload-queue" aria-live="polite">
            <article v-for="(item, index) in queue" :key="item.local_id" class="upload-queue-item">
              <div class="queue-heading"><div><strong>{{ index + 1 }}. {{ queueKind(item) }}</strong><small v-if="item.file">{{ item.file.name }}·{{ formatBytes(item.file.size) }}</small></div><span class="queue-status" :class="`status-${item.status}`">{{ queueStatus(item.status) }}</span></div>
              <label><span>标题（可选）</span><input v-model="item.title" :aria-label="`第 ${index + 1} 项标题`" maxlength="200" :disabled="!editableQueueContent(item)"></label>
              <label v-if="item.kind === 'text'"><span>纯文本</span><textarea v-model="item.text" :aria-label="`第 ${index + 1} 项文本`" maxlength="100000" rows="5" :disabled="!editableQueueContent(item)"></textarea></label>
              <label v-if="item.kind === 'url'"><span>HTTP(S) 网址</span><input v-model="item.url" type="url" :aria-label="`第 ${index + 1} 项网址`" maxlength="2048" placeholder="https://example.com/manual" :disabled="!editableQueueContent(item)"></label>
              <p v-if="item.duplicate" class="duplicate-note">这个文件与队列中的名称、大小和修改时间相同，仍已保留。</p>
              <progress v-if="item.status === 'uploading'" :value="item.progress" max="100">{{ item.progress }}%</progress>
              <p v-if="item.error" class="queue-error">{{ item.error }}</p>
              <div class="queue-actions">
                <el-button :disabled="index === 0 || !editableQueueItem(item)" @click="moveQueue(index, -1)">上移</el-button>
                <el-button :disabled="index === queue.length - 1 || !editableQueueItem(item)" @click="moveQueue(index, 1)">下移</el-button>
                <el-button v-if="item.status === 'waiting' || item.status === 'failed'" type="danger" plain :disabled="saving || uploading" @click="removeQueue(index)">移除</el-button>
              </div>
            </article>
          </div>
          <el-empty v-if="queue.length === 0" description="还没有待处理资料" :image-size="70" />
          <div class="primary-actions">
            <el-button v-if="showUploadAction" type="primary" size="large" :loading="saving || uploading" :disabled="saving || uploading" @click="saveAndUpload">{{ primaryActionText }}</el-button>
          </div>
        </section>

        <section v-if="manual" class="card editor-section saved-items">
          <div class="section-title"><div><h2>已保存资料</h2><p>调整顺序或封面后，点击“保存设置与顺序”。</p></div><span>{{ manual.items?.length || 0 }} 项</span></div>
          <fieldset class="cover-field" :disabled="saving || uploading"><legend>封面</legend><label><input v-model="coverSelection" type="radio" value="auto" name="cover" @change="coverDirty = true"><span>自动选择（新添图片可自动成为封面）</span></label></fieldset>
          <div class="saved-list">
            <article v-for="(item, index) in manual.items" :key="item.id" class="saved-item">
              <label class="cover-choice"><input v-model="coverSelection" type="radio" :value="String(item.id)" name="cover" :disabled="saving || uploading" @change="coverDirty = true"><span>选为封面</span></label>
              <div class="saved-summary"><strong>{{ index + 1 }}. {{ item.title || item.original_name || queueKind(item) }}</strong><small>{{ queueKind(item) }}</small></div>
              <div class="saved-actions"><el-button :disabled="saving || uploading || index === 0" @click="moveSaved(index, -1)">上移</el-button><el-button :disabled="saving || uploading || index === manual.items.length - 1" @click="moveSaved(index, 1)">下移</el-button><el-button type="danger" plain :disabled="saving || uploading || (manual.status === 'active' && manual.items.length <= 1)" @click="deleteSavedItem(item)">删除</el-button></div>
            </article>
          </div>
          <el-empty v-if="!manual.items?.length" description="草稿还没有已保存资料" :image-size="70" />
        </section>

        <section v-if="manual" class="danger-zone"><div><h2>删除说明书</h2><p>删除后立即不可访问，已占用的物理空间不会立即释放。</p></div><el-button type="danger" plain :disabled="saving || uploading" @click="deleteManual">删除整份说明书</el-button></section>
      </template>
    </main>
  </div>
</template>

<script>
import MyHeader from '@/components/MyHeader.vue'

const {manualsApi} = require('@/api/manuals.cjs')
const {appendFileItems, appendStructuredItem, createRequestID, moveItem, normalizeManualCategories, retryFailedItem, safeExternalURL, uploadQueuedItems} = require('@/utils/manuals_behavior.cjs')
const {resourcePasswordError} = require('@/utils/file_sharing_behavior.cjs')

export default {
  name: 'ManualEditor',
  components: {MyHeader},
  data() {
    return {
      manual: null, form: {name: '', description: '', categories: [], access_mode: 'owner'}, categories: [], queue: [],
      coverSelection: 'auto', coverDirty: false, creationRequestID: createRequestID(), creationPayload: null, conflictLatest: null,
      loading: false, saving: false, uploading: false, fatalError: '', notice: '', noticeType: 'info', localFilesLost: false,
      passwordState: {password_protected: false, version: 0}, passwordDraft: '', passwordConfirmation: '', passwordSaving: false, passwordNotice: '', passwordNoticeType: 'info',
      accessOptions: [
        {value: 'owner', label: '仅自己', description: '默认选择，只有你可查看和编辑'},
        {value: 'authenticated', label: '登录用户', description: '本站有效登录用户可查看'},
        {value: 'public', label: '所有人', description: '拿到链接的访客无需登录即可查看'},
      ],
    }
  },
  computed: {
    manualID() { return this.manual?.id || this.$route.params.id || '' },
    totalItemCount() { return (this.manual?.items?.length || 0) + this.queue.filter(item => item.status !== 'success').length },
    hasFailures() { return this.queue.some(item => item.status === 'failed') },
    hasWaiting() { return this.queue.some(item => item.status === 'waiting' || item.status === 'failed') },
    showUploadAction() { return !this.manual || this.manual.status === 'draft' || this.hasWaiting },
    primaryActionText() {
      if (this.hasFailures) return '重试失败项'
      if (!this.manual) return '创建并发布'
      if (this.manual.status === 'draft') return this.hasWaiting ? '继续上传并发布' : '发布说明书'
      return '上传待处理资料'
    },
  },
  async created() {
    if (this.requireAccess()) return
    this.localFilesLost = this.readInterruptedMarker()
    this.loading = Boolean(this.$route.params.id)
    try {
      await this.loadCategories()
      if (this.$route.params.id) await this.loadManual()
    } catch (error) {
      this.fatalError = error.message || '编辑器加载失败'
    } finally { this.loading = false }
  },
  methods: {
    requireAccess() {
      const user = this.$store.state.userInfo
      if (!user) { this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}}); return true }
      if (user.capabilities?.manuals === false) { this.$router.replace({path: '/forbidden', query: {feature: 'manuals', reason: 'disabled'}}); return true }
      return false
    },
    async loadCategories() {
      const data = await manualsApi.listAllCategories({mine: true})
      this.categories = [...new Set((data.items || []).filter(Boolean))]
    },
    async loadManual() {
      const manual = await manualsApi.getManual(String(this.$route.params.id))
      if (!manual.can_edit) throw new Error('你不能编辑这份说明书')
      this.applyManual(manual)
      await this.loadManualPassword()
    },
    async loadManualPassword() {
      const state = await manualsApi.getPassword(this.manualID)
      this.passwordState = {password_protected: Boolean(state.password_protected), version: Number(state.version || 0)}
    },
    async saveManualPassword() {
      if (this.passwordSaving) return
      const validation = resourcePasswordError(this.passwordDraft, this.passwordConfirmation)
      if (validation) { this.passwordNotice = validation; this.passwordNoticeType = 'error'; return }
      await this.updateManualPassword(this.passwordDraft)
    },
    async clearManualPassword() {
      try { await this.$confirm('清除后，满足原可见范围的访问者无需再输入阅读密码。', '清除阅读密码', {type: 'warning', confirmButtonText: '确认清除'}) }
      catch (_) { return }
      await this.updateManualPassword('')
    },
    async updateManualPassword(password) {
      this.passwordSaving = true; this.passwordNotice = ''
      try {
        const state = await manualsApi.setPassword(this.manualID, {password, version: this.passwordState.version})
        this.passwordState = {password_protected: Boolean(state.password_protected), version: Number(state.version || 0)}
        this.passwordDraft = ''; this.passwordConfirmation = ''; this.passwordNotice = state.password_protected ? '阅读密码已更新，旧解锁状态立即失效。' : '阅读密码已清除。'; this.passwordNoticeType = 'success'
      } catch (error) {
        if (error.status === 409) { await this.loadManualPassword(); this.passwordNotice = '密码设置已被其他操作修改，请重新填写后保存。'; this.passwordNoticeType = 'warning' }
        else if (error.status === 401) { this.$store.commit('REMOVE_INFO'); this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}}) }
        else { this.passwordNotice = error.message || '阅读密码更新失败'; this.passwordNoticeType = 'error' }
      } finally { this.passwordSaving = false }
    },
    applyManual(manual, keepForm = false) {
      const draft = {...this.form}
      this.manual = {...manual, id: String(manual.id), items: (manual.items || []).slice().sort((a, b) => Number(a.position || 0) - Number(b.position || 0))}
      if (!keepForm) this.form = {name: manual.name || '', description: manual.description || '', categories: (manual.categories || []).slice(), access_mode: manual.access_mode || 'owner'}
      else this.form = draft
      this.coverSelection = manual.cover_item_id == null ? 'auto' : String(manual.cover_item_id)
      this.coverDirty = false
    },
    appendFiles(event) {
      if (this.saving || this.uploading) { if (event?.target) event.target.value = ''; return }
      const result = appendFileItems(this.queue, event.target.files, this.manual?.items?.length || 0)
      this.queue = result.items
      event.target.value = ''
      if (result.errors.length) this.showNotice(result.errors.join('；'), 'error')
      if (this.queue.some(item => item.file && item.status !== 'success')) this.markInterrupted()
    },
    appendStructured(kind) {
      if (this.saving || this.uploading) return
      const result = appendStructuredItem(this.queue, kind, this.manual?.items?.length || 0)
      this.queue = result.items
      if (result.errors.length) this.showNotice(result.errors.join('；'), 'error')
    },
    moveQueue(index, offset) { if (this.saving || this.uploading) return; this.queue = moveItem(this.queue, index, offset) },
    removeQueue(index) { if (this.saving || this.uploading) return; this.queue.splice(index, 1); this.updateInterruptedMarker() },
    moveSaved(index, offset) { if (this.saving || this.uploading) return; this.manual.items = moveItem(this.manual.items, index, offset) },
    editableQueueItem(item) { return !this.saving && !this.uploading && ['waiting', 'failed'].includes(item.status) },
    editableQueueContent(item) { return !this.saving && !this.uploading && item.status === 'waiting' && !item.attempted },
    queueStatus(status) { return {waiting: '等待上传', uploading: '上传中', success: '已成功', failed: '上传失败'}[status] || status },
    queueKind(item) { return {image: '图片', file: item.file?.name?.toLowerCase().endsWith('.txt') ? 'TXT 文本' : item.file?.name?.toLowerCase().endsWith('.pdf') ? 'PDF' : '文件', pdf: 'PDF', text: '文本', url: '网页链接'}[item.kind] || '资料' },
    formatBytes(bytes) { const size = Number(bytes || 0); return size < 1024 * 1024 ? `${(size / 1024).toFixed(1)} KiB` : `${(size / 1024 / 1024).toFixed(1)} MiB` },
    showNotice(message, type = 'info') { this.notice = message; this.noticeType = type },
    validateBeforeSave() {
      if (!this.form.name.trim()) return '请填写说明书名称'
      if (Array.from(this.form.name.trim()).length > 120) return '说明书名称不能超过 120 个字符'
      try { normalizeManualCategories(this.form.categories) } catch (error) { return error.message }
      if (Array.from(this.form.description).length > 2000) return '说明不能超过 2000 个字符'
      for (const item of this.queue.filter(value => ['waiting', 'failed'].includes(value.status))) {
        if (Array.from(item.title || '').length > 200) return '资料标题不能超过 200 个字符'
        if (item.kind === 'text' && !item.text.trim()) return '请填写待上传的文本内容'
        if (item.kind === 'url' && !safeExternalURL(item.url)) return '网址必须是不含用户名和密码的 HTTP(S) 链接'
      }
      return ''
    },
    metadataPayload() { return {name: this.form.name.trim(), description: this.form.description, categories: normalizeManualCategories(this.form.categories), access_mode: this.form.access_mode} },
    async ensureDraft() {
      if (this.manual) return
      if (!this.creationPayload) this.creationPayload = {...this.metadataPayload(), client_request_id: this.creationRequestID}
      this.applyManual(await manualsApi.createManual(this.creationPayload), true)
      this.$router.replace(`/manuals/${this.manual.id}/edit`)
      try { sessionStorage.removeItem(this.storageKey('new')) } catch (error) { /* storage may be unavailable */ }
      this.markInterrupted()
    },
    async uploadOne(item) {
      let result
      if (item.file) {
        result = await manualsApi.uploadFile(this.manual.id, item.file, item.title.trim(), item.client_request_id, event => {
          if (event.total) item.progress = Math.min(99, Math.round(event.loaded / event.total * 100))
        })
      } else {
        const payload = {kind: item.kind, title: item.title.trim(), client_request_id: item.client_request_id}
        if (item.kind === 'text') payload.text = item.text
        else payload.url = safeExternalURL(item.url)
        result = await manualsApi.addItem(this.manual.id, payload)
      }
      this.manual.revision = result.revision
      if (!this.manual.items.some(value => String(value.id) === String(result.item.id))) this.manual.items.push({...result.item, id: String(result.item.id)})
      return result
    },
    orderedItemIDs() {
      const uploaded = this.queue.filter(item => item.status === 'success' && item.server_item).map(item => String(item.server_item.id))
      const uploadedSet = new Set(uploaded)
      return this.manual.items.map(item => String(item.id)).filter(id => !uploadedSet.has(id)).concat(uploaded)
    },
    patchPayload(itemIDs, activate = false) {
      const payload = {revision: Number(this.manual.revision), ...this.metadataPayload(), item_ids: itemIDs}
      if (activate) payload.status = 'active'
      if (this.coverDirty) payload.cover_item_id = this.coverSelection === 'auto' ? null : this.coverSelection
      return payload
    },
    async applyPatch(payload) {
      const previousItems = this.manual.items.slice()
      const updated = await manualsApi.updateManual(this.manual.id, payload)
      const ordered = payload.item_ids.map(id => previousItems.find(item => String(item.id) === String(id))).filter(Boolean)
      this.applyManual({...this.manual, ...updated, items: updated?.items?.length ? updated.items : ordered})
    },
    async saveAndUpload() {
      if (this.saving || this.uploading) return
      const validation = this.validateBeforeSave()
      if (validation) { this.showNotice(validation, 'error'); return }
      if (!this.manual && this.queue.length === 0) { this.showNotice('请至少添加一项资料', 'error'); return }
      this.saving = true
      this.uploading = true
      this.conflictLatest = null
      try {
        await this.ensureDraft()
        this.queue.forEach(item => retryFailedItem(item))
        await uploadQueuedItems(this.queue, item => this.uploadOne(item))
        const incomplete = this.queue.filter(item => item.status !== 'success')
        if (incomplete.length) {
          const failedCount = incomplete.filter(item => item.status === 'failed').length
          if (failedCount) {
            const result = this.manual.status === 'draft' ? '已成功项已保留，草稿未发布。' : '成功追加项已可见，失败项未保存。'
            this.showNotice(`${failedCount} 项资料上传失败；${result}`, 'error')
          } else this.showNotice(`仍有 ${incomplete.length} 项资料未上传成功，已停止保存。`, 'error')
          this.markInterrupted()
          return
        }
        const itemIDs = this.orderedItemIDs()
        if (this.manual.status === 'draft' && itemIDs.length === 0) { this.showNotice('请至少添加一项资料，草稿未发布。', 'error'); return }
        const publishing = this.manual.status === 'draft'
        if (publishing) await this.applyPatch(this.patchPayload(itemIDs, true))
        else await this.applyPatch(this.patchPayload(itemIDs, false))
        this.queue = []
        this.clearInterruptedMarkers()
        this.showNotice(publishing ? '说明书已发布' : '资料已上传，设置与顺序已保存', 'success')
      } catch (error) { await this.handleMutationError(error) }
      finally { this.saving = false; this.uploading = false }
    },
    async saveSettings() {
      if (this.saving || this.uploading || !this.manual) return
      const validation = this.validateBeforeSave()
      if (validation) { this.showNotice(validation, 'error'); return }
      this.saving = true
      this.conflictLatest = null
      try {
        await this.applyPatch(this.patchPayload(this.manual.items.map(item => String(item.id))))
        this.showNotice('设置与资料顺序已保存', 'success')
      } catch (error) { await this.handleMutationError(error) }
      finally { this.saving = false }
    },
    async handleMutationError(error) {
      if (error.status === 409) {
        try { this.conflictLatest = await manualsApi.getManual(this.manual.id); this.showNotice('检测到版本冲突，本地草稿已保留。', 'warning') }
        catch (reloadError) { this.showNotice(reloadError.message || '版本冲突，且最新数据读取失败', 'error') }
        return
      }
      if (error.status === 401) { this.$store.commit('REMOVE_INFO'); this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}}); return }
      this.showNotice(error.message || '操作失败', 'error')
    },
    continueFromLatest() {
      if (this.saving || this.uploading || !this.conflictLatest) return
      const latest = this.conflictLatest
      this.conflictLatest = null
      this.applyManual(latest, true)
      this.showNotice('已载入最新修订；本地元数据草稿和待上传队列仍在，请重新检查后保存。', 'warning')
    },
    async deleteSavedItem(item) {
      if (this.saving || this.uploading) return
      try { await this.$confirm(`确定删除“${item.title || item.original_name || '这项资料'}”？`, '删除资料', {type: 'warning'}) }
      catch (error) { return }
      if (this.saving || this.uploading) return
      this.saving = true
      const draft = {...this.form}
      try {
        await manualsApi.deleteItem(this.manual.id, String(item.id), Number(this.manual.revision))
        const latest = await manualsApi.getManual(this.manual.id)
        this.queue = this.queue.filter(queueItem => String(queueItem.server_item?.id || '') !== String(item.id))
        this.applyManual(latest)
        this.form = draft
        this.showNotice('资料已删除', 'success')
      } catch (error) { await this.handleMutationError(error) }
      finally { this.saving = false }
    },
    async deleteManual() {
      if (this.saving || this.uploading) return
      try { await this.$confirm('删除后说明书立即不可访问。', '删除说明书', {type: 'error', confirmButtonText: '确认删除'}) }
      catch (error) { return }
      if (this.saving || this.uploading) return
      this.saving = true
      try { await manualsApi.deleteManual(this.manual.id, Number(this.manual.revision)); this.clearInterruptedMarkers(); await this.$router.replace('/manuals') }
      catch (error) { await this.handleMutationError(error) }
      finally { this.saving = false }
    },
    storageKey(id = this.manualID || 'new') { return `manual-editor-pending:${id}` },
    readInterruptedMarker() { try { return sessionStorage.getItem(this.storageKey()) === '1' } catch (error) { return false } },
    markInterrupted() { try { if (this.queue.some(item => item.file && item.status !== 'success')) sessionStorage.setItem(this.storageKey(), '1') } catch (error) { return } },
    updateInterruptedMarker() { if (this.queue.some(item => item.file && item.status !== 'success')) this.markInterrupted(); else this.clearInterruptedMarkers() },
    clearInterruptedMarkers() { try { sessionStorage.removeItem(this.storageKey()); sessionStorage.removeItem(this.storageKey('new')); this.localFilesLost = false } catch (error) { return } },
  },
}
</script>

<style scoped>
.manual-editor-page{max-width:940px}.editor-loading{min-height:240px;padding:80px;text-align:center;color:var(--text-secondary)}.editor-heading{display:flex;align-items:flex-end;justify-content:space-between;gap:20px;margin-bottom:26px}.editor-heading h1{margin:0;font-size:clamp(26px,5vw,36px);overflow-wrap:anywhere}.editor-heading p{margin:10px 0 0;color:var(--text-secondary);font-size:14px}.detail-link{flex-shrink:0;text-decoration:none}.editor-alert{margin:0 0 18px}.editor-section{margin-bottom:22px}.editor-section>h2,.conflict-card h2{margin:0 0 20px;font-size:19px}.metadata-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(180px,.45fr);gap:18px}.metadata-grid label,.upload-queue-item label{display:block;min-width:0}.metadata-grid label>span,.upload-queue-item label>span,.password-grid label>span{display:block;margin-bottom:7px;color:var(--text-secondary);font-size:13px;font-weight:600}.wide-field{grid-column:1/-1}.category-field .el-select{width:100%}.metadata-grid input,.metadata-grid textarea,.upload-queue-item input,.upload-queue-item textarea{width:100%;min-height:44px;border:1px solid var(--border-color);border-radius:9px;background:#fcfdfc;color:var(--text-primary);font:inherit;padding:10px 12px;resize:vertical}.access-field,.cover-field{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:10px;margin:22px 0 0;border:0;padding:0}.access-field legend,.cover-field legend{grid-column:1/-1;margin-bottom:8px;font-size:14px;font-weight:650}.access-field label,.cover-field label{display:flex;align-items:flex-start;gap:9px;border:1px solid var(--border-color);border-radius:9px;padding:12px}.access-field input,.cover-field input{width:18px;height:18px;flex-shrink:0}.access-field strong,.access-field small{display:block}.access-field small{margin-top:3px;color:var(--text-secondary);line-height:1.5}.section-actions{display:flex;justify-content:flex-end;margin-top:20px}.section-title{display:flex;align-items:flex-start;justify-content:space-between;gap:16px;margin-bottom:18px}.section-title h2{margin:0;font-size:19px}.section-title p{margin:5px 0 0;color:var(--text-secondary);font-size:13px}.section-title>span{flex-shrink:0;color:var(--text-secondary);font-size:13px}.password-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.password-grid label{min-width:0}.password-alert{margin-top:16px}.password-actions{gap:8px}.password-actions .el-button{margin:0}.source-actions{display:flex;flex-wrap:wrap;gap:10px}.source-actions .el-button{margin-left:0;min-height:44px}.file-picker{display:inline-flex;min-height:44px;align-items:center;border:1px solid #9abdaf;border-radius:8px;background:#f0f6f2;color:var(--primary-dark);cursor:pointer;font-size:14px;font-weight:600;padding:0 15px}.file-picker.disabled{cursor:not-allowed;filter:grayscale(.35);opacity:.6}.file-picker input{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);clip-path:inset(50%)}.format-note{margin:14px 0;color:var(--text-secondary);font-size:12px}.upload-queue{display:grid;gap:14px}.upload-queue-item{min-width:0;border:1px solid var(--border-color);border-radius:12px;background:#fbfcfa;padding:16px}.queue-heading{display:flex;align-items:flex-start;justify-content:space-between;gap:12px;margin-bottom:13px}.queue-heading strong,.queue-heading small{display:block;overflow-wrap:anywhere}.queue-heading small{margin-top:3px;color:var(--text-secondary)}.queue-status{flex-shrink:0;border-radius:999px;background:#e8efea;color:var(--text-secondary);font-size:12px;padding:3px 9px}.status-success{background:#e4f3e9;color:#277044}.status-failed{background:#fff0eb;color:#a2432c}.status-uploading{background:#e7f1f5;color:#2e6579}.upload-queue-item label+label{margin-top:13px}.upload-queue-item progress{width:100%;margin-top:13px}.duplicate-note{color:#956b2d}.queue-error{color:#a2432c;overflow-wrap:anywhere}.queue-actions,.saved-actions{display:flex;flex-wrap:wrap;gap:8px;margin-top:14px}.queue-actions .el-button,.saved-actions .el-button{min-height:40px;margin-left:0}.primary-actions{display:flex;justify-content:flex-end;margin-top:20px}.primary-actions .el-button{min-height:46px}.cover-field{grid-template-columns:1fr}.saved-list{display:grid;gap:10px;margin-top:14px}.saved-item{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:14px;border-top:1px solid var(--border-color);padding:14px 0}.cover-choice{display:flex;align-items:center;gap:6px;font-size:12px}.cover-choice input{width:18px;height:18px}.saved-summary{min-width:0}.saved-summary strong,.saved-summary small{display:block;overflow-wrap:anywhere}.saved-summary small{color:var(--text-secondary)}.saved-actions{margin-top:0}.conflict-card{margin-bottom:20px;border-color:#e6bf7f;background:#fffaf0}.conflict-card p{color:#795b2b}.conflict-card dl{display:grid;grid-template-columns:100px minmax(0,1fr);gap:8px;font-size:13px}.conflict-card dd{margin:0;overflow-wrap:anywhere}.danger-zone{display:flex;align-items:center;justify-content:space-between;gap:20px;margin-top:34px;border:1px solid #efd2c9;border-radius:14px;background:#fff8f5;padding:20px}.danger-zone h2{margin:0;font-size:17px}.danger-zone p{margin:5px 0 0;color:#8c584a;font-size:13px}@media(max-width:640px){.editor-heading{align-items:stretch;flex-direction:column}.metadata-grid,.password-grid{grid-template-columns:minmax(0,1fr)}.wide-field{grid-column:auto}.access-field{grid-template-columns:1fr}.source-actions>*{width:100%;justify-content:center}.saved-item{grid-template-columns:minmax(0,1fr)}.saved-actions,.queue-actions{display:grid;grid-template-columns:repeat(2,minmax(0,1fr))}.saved-actions .el-button,.queue-actions .el-button{width:100%}.cover-choice{min-height:40px}.primary-actions .el-button,.section-actions .el-button{width:100%;min-height:46px}.danger-zone{align-items:stretch;flex-direction:column}.danger-zone .el-button{width:100%;min-height:44px}.editor-section{padding:20px 14px}.conflict-card dl{grid-template-columns:1fr}.conflict-card dt{font-weight:650}.conflict-card .el-button{width:100%;white-space:normal;height:auto;min-height:44px}}
</style>
