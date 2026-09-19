<template>
  <div>
    <MyHeader />
    <main class="page-container applications-page">
      <div class="action-bar applications-heading">
        <div>
          <span class="page-eyebrow">APPLICATION CREDENTIALS</span>
          <h1 class="page-title">应用凭证</h1>
          <p class="page-desc">为脚本和自动化创建独立凭证，并只授予必需权限。</p>
        </div>
        <el-button type="primary" size="large" @click="openCreate">
          <el-icon><Plus /></el-icon>创建应用
        </el-button>
      </div>

      <el-alert
        title="Secret Key 只在创建或轮换后显示一次。关闭提示前请复制或下载，之后无法找回。"
        type="info"
        :closable="false"
        show-icon
        class="credential-note"
      />

      <section class="card applications-card" aria-label="应用凭证列表">
        <div class="list-toolbar">
          <span>仅显示你创建的应用，包括已永久吊销的审计记录。</span>
          <el-button :loading="loading" @click="loadApplications(true)"><el-icon><Refresh /></el-icon>刷新</el-button>
        </div>

        <el-table v-loading="loading" :data="applications" class="desktop-applications">
          <el-table-column label="应用 / Access Key" min-width="260">
            <template #default="scope">
              <div class="application-identity">
                <span class="application-icon"><el-icon><Key /></el-icon></span>
                <div class="application-text">
                  <strong>{{ scope.row.name }}</strong>
                  <code>{{ scope.row.access_key }}</code>
                  <small v-if="scope.row.description">{{ scope.row.description }}</small>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="权限" min-width="220">
            <template #default="scope">
              <div class="scope-tags">
                <el-tag v-for="item in scope.row.scopes" :key="item" size="small" :type="item.endsWith(':write') ? 'warning' : 'info'">
                  {{ scopeText(item) }}
                </el-tag>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="状态 / 到期" min-width="170">
            <template #default="scope">
              <el-tag :type="statusTagType(scope.row.status)">{{ statusText[scope.row.status] || scope.row.status }}</el-tag>
              <div class="date-text">{{ expiryText(scope.row) }}</div>
            </template>
          </el-table-column>
          <el-table-column fixed="right" label="操作" width="310" align="right">
            <template #default="scope">
              <span v-if="scope.row.status === 'revoked'" class="revoked-note">不可恢复</span>
              <div v-else class="row-actions">
                <el-button size="small" @click="openEdit(scope.row)">编辑</el-button>
                <el-button size="small" :loading="mutatingID === scope.row.id" @click="toggleStatus(scope.row)">{{ scope.row.status === 'enabled' ? '停用' : '启用' }}</el-button>
                <el-button size="small" @click="rotateSecret(scope.row)">轮换</el-button>
                <el-button size="small" type="danger" plain @click="revokeApplication(scope.row)">吊销</el-button>
              </div>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty description="还没有应用凭证" :image-size="90">
              <el-button type="primary" plain @click="openCreate">创建应用</el-button>
            </el-empty>
          </template>
        </el-table>

        <div v-loading="loading" class="mobile-applications">
          <article v-for="application in applications" :key="application.id" class="mobile-application">
            <div class="application-identity">
              <span class="application-icon"><el-icon><Key /></el-icon></span>
              <div class="application-text">
                <strong>{{ application.name }}</strong>
                <code>{{ application.access_key }}</code>
                <small v-if="application.description">{{ application.description }}</small>
              </div>
            </div>
            <div class="mobile-meta">
              <el-tag :type="statusTagType(application.status)">{{ statusText[application.status] || application.status }}</el-tag>
              <span>{{ expiryText(application) }}</span>
            </div>
            <div class="scope-tags">
              <el-tag v-for="item in application.scopes" :key="item" size="small" :type="item.endsWith(':write') ? 'warning' : 'info'">
                {{ scopeText(item) }}
              </el-tag>
            </div>
            <div v-if="application.status !== 'revoked'" class="mobile-actions row-actions">
              <el-button size="small" @click="openEdit(application)">编辑</el-button>
              <el-button size="small" :loading="mutatingID === application.id" @click="toggleStatus(application)">{{ application.status === 'enabled' ? '停用' : '启用' }}</el-button>
              <el-button size="small" @click="rotateSecret(application)">轮换</el-button>
              <el-button size="small" type="danger" plain @click="revokeApplication(application)">吊销</el-button>
            </div>
          </article>
          <el-empty v-if="!loading && applications.length === 0" description="还没有应用凭证" :image-size="80" />
        </div>
        <div v-if="hasMore" class="load-more"><el-button :loading="loadingMore" @click="loadMore">加载更多</el-button></div>
      </section>

      <el-dialog v-model="formDialogVisible" :title="editingApplication ? '编辑应用' : '创建应用'" width="640px" destroy-on-close>
        <el-form ref="applicationForm" :model="form" :rules="rules" label-position="top">
          <el-form-item label="应用名称" prop="name">
            <el-input v-model="form.name" maxlength="128" show-word-limit />
          </el-form-item>
          <el-form-item label="应用说明" prop="description">
            <el-input v-model="form.description" type="textarea" :rows="3" maxlength="2000" show-word-limit />
          </el-form-item>
          <el-form-item label="权限范围" prop="scopes">
            <el-checkbox-group v-model="form.scopes" class="scope-options" @change="normalizeSelectedScopes">
              <el-checkbox
                v-for="scope in availableScopeOptions"
                :key="scope.name"
                :label="scope.name"
                :disabled="scope.action === 'read' && writeSelected(scope.resource)"
                border
              >
                <span class="scope-name">{{ scope.label }}</span>
                <small>{{ scope.description }}</small>
              </el-checkbox>
            </el-checkbox-group>
          </el-form-item>
          <el-alert title="写入权限会自动包含同一服务的读取权限。" type="warning" :closable="false" class="scope-note" />
          <p class="muted">每人最多保留 {{ maxApplications }} 个未吊销应用；有效期最长 {{ maxCredentialTTLDays }} 天。</p>
          <el-form-item :label="editingApplication ? '重新设置有效期（可选）' : '有效期'" prop="expiresInDays">
            <el-input-number v-model="form.expiresInDays" :min="1" :max="maxCredentialTTLDays" controls-position="right" />
            <span class="field-suffix">天{{ editingApplication ? '；留空则保持原到期时间' : '' }}</span>
          </el-form-item>
        </el-form>
        <template #footer>
          <el-button @click="formDialogVisible = false">取消</el-button>
          <el-button type="primary" :loading="saving" @click="saveApplication">{{ editingApplication ? '保存' : '创建并生成密钥' }}</el-button>
        </template>
      </el-dialog>

      <el-dialog
        v-model="secretDialogVisible"
        title="请立即保存应用凭证"
        width="620px"
        destroy-on-close
        :close-on-click-modal="false"
        :before-close="beforeCredentialClose"
        @closed="clearCredential"
      >
        <template v-if="oneTimeCredential">
          <el-alert title="Secret Key 离开此窗口后无法再次查看。请勿把凭证放入 URL、日志或前端代码。" type="warning" :closable="false" show-icon />
          <dl class="credential-values">
            <div><dt>Access Key</dt><dd>{{ oneTimeCredential.accessKey }}</dd></div>
            <div><dt>Secret Key</dt><dd>{{ oneTimeCredential.secretKey }}</dd></div>
          </dl>
        </template>
        <template #footer>
          <el-button @click="copyCredential">复制 JSON</el-button>
          <el-button @click="downloadCredential">下载 JSON</el-button>
          <el-button type="primary" @click="closeCredential">我已保存，关闭</el-button>
        </template>
      </el-dialog>
    </main>
  </div>
</template>

<script>
import {ElMessage, ElMessageBox} from 'element-plus'
import {Key, Plus, Refresh} from '@element-plus/icons-vue'
import MyHeader from '@/components/MyHeader'

const {applicationsApi} = require('@/api/applications.cjs')
const {
  clearOneTimeCredential,
  createOneTimeCredential,
  credentialJSON,
  handleIdentityFailure,
  normalizeScopes,
} = require('@/utils/applications_behavior.cjs')

export default {
  name: 'ApplicationsPage',
  components: {MyHeader, Key, Plus, Refresh},
  // 初始化凭证列表、分页和编辑弹窗状态，并提供权限选项、有效期表单及一次性密钥展示所需的数据。
  data() {
    return {
      loading: false,
      loadingMore: false,
      saving: false,
      mutatingID: '',
      applications: [],
      nextCursor: '',
      hasMore: false,
      formDialogVisible: false,
      secretDialogVisible: false,
      editingApplication: null,
      oneTimeCredential: null,
      form: {name: '', description: '', scopes: ['web-projects:read'], expiresInDays: 90},
      rules: {
        name: [{required: true, message: '请输入应用名称', trigger: 'blur'}],
        scopes: [{type: 'array', required: true, message: '请选择至少一个权限', trigger: 'change'}],
        expiresInDays: [{required: true, message: '请设置有效期', trigger: 'change'}],
      },
      statusText: {enabled: '已启用', disabled: '已停用', revoked: '已永久吊销'},
      scopeOptions: [
        {name: 'web-comments:read', resource: 'web-comments', action: 'read', label: '网页评论 · 读取', description: '读取可见网页的评论与历史'},
        {name: 'web-comments:write', resource: 'web-comments', action: 'write', label: '网页评论 · 写入', description: '创建、回复、解决和重开评论'},
        {name: 'web-projects:read', resource: 'web-projects', action: 'read', label: '网页托管 · 读取', description: '查看托管内容与版本'},
        {name: 'web-projects:write', resource: 'web-projects', action: 'write', label: '网页托管 · 写入', description: '可发布、修改和删除托管内容'},
        {name: 'library:read', resource: 'library', action: 'read', label: '家庭藏书 · 读取', description: '查看图书信息'},
        {name: 'library:write', resource: 'library', action: 'write', label: '家庭藏书 · 写入', description: '可新增、修改和删除图书'},
        {name: 'manuals:read', resource: 'manuals', action: 'read', label: '说明书管理 · 读取', description: '查看有权访问的说明书与资料'},
        {name: 'manuals:write', resource: 'manuals', action: 'write', label: '说明书管理 · 写入', description: '可新增、修改和删除自己的说明书'},
        {name: 'webdav:read', resource: 'webdav', action: 'read', label: 'WebDAV · 读取', description: '读取 WebDAV 文件'},
        {name: 'webdav:write', resource: 'webdav', action: 'write', label: 'WebDAV · 写入', description: '可新增、覆盖和删除文件'},
      ],
    }
  },
  computed: {
    defaultCredentialTTLDays() { return this.$store.state.userInfo?.application_policy?.default_credential_ttl_days || 90 },
    maxCredentialTTLDays() { return this.$store.state.userInfo?.application_policy?.max_credential_ttl_days || 365 },
    maxApplications() { return this.$store.state.userInfo?.application_policy?.max_applications_per_user || 20 },
    availableScopeOptions() {
      const user = this.$store.state.userInfo
      return this.scopeOptions.filter(option => {
        if (this.form.scopes.includes(option.name)) return true
        if (option.resource === 'library') return user?.library_enabled && user?.capabilities?.library !== false
        if (option.resource === 'manuals') return this.$store.state.modules?.manuals === true && user?.capabilities?.manuals !== false
        if (option.resource === 'webdav') return user?.webdav_permission !== 'none' && user?.capabilities?.webdav !== false && (option.action === 'read' || user?.webdav_permission === 'write')
        return true
      })
    },
  },
  created() {
    if (!this.requireLogin()) {
      this.loadApplications(true)
    }
  },
  beforeUnmount() {
    this.clearCredential()
  },
  methods: {
    requireLogin() {
      if (this.$store.state.userInfo) {
        return false
      }
      this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
      return true
    },
    async loadApplications(reset) {
      if (this.requireLogin()) {
        return
      }
      this.loading = true
      try {
        const data = await applicationsApi.listApplications({cursor: reset ? '' : this.nextCursor, limit: 20})
        this.applications = reset ? data.items : this.applications.concat(data.items)
        this.nextCursor = data.next_cursor || ''
        this.hasMore = Boolean(data.has_more)
      } catch (error) {
        this.handleError(error)
      } finally {
        this.loading = false
        this.loadingMore = false
      }
    },
    loadMore() {
      this.loadingMore = true
      this.loadApplications(false)
    },
    openCreate() {
      this.editingApplication = null
      this.form = {name: '', description: '', scopes: ['web-projects:read'], expiresInDays: this.defaultCredentialTTLDays}
      this.rules.expiresInDays[0].required = true
      this.formDialogVisible = true
    },
    openEdit(application) {
      this.editingApplication = application
      this.form = {
        name: application.name,
        description: application.description || '',
        scopes: normalizeScopes(application.scopes),
        expiresInDays: null,
      }
      this.rules.expiresInDays[0].required = false
      this.formDialogVisible = true
    },
    normalizeSelectedScopes(scopes) {
      this.form.scopes = normalizeScopes(scopes)
    },
    writeSelected(resource) {
      return this.form.scopes.includes(`${resource}:write`)
    },
    // 校验表单并规范权限集合；编辑时使用当前修订号，新建时展示仅本次返回的密钥并刷新列表。
    async saveApplication() {
      try {
        await this.$refs.applicationForm.validate()
      } catch (error) {
        return
      }
      this.saving = true
      const payload = {
        name: this.form.name.trim(),
        description: this.form.description.trim(),
        scopes: normalizeScopes(this.form.scopes),
      }
      if (!this.editingApplication || this.form.expiresInDays !== null) {
        payload.expires_in_days = this.form.expiresInDays
      }
      try {
        if (this.editingApplication) {
          const application = await applicationsApi.updateApplication(this.editingApplication.id, this.editingApplication.revision, payload)
          this.replaceApplication(application)
          ElMessage.success('应用设置已保存')
        } else {
          const result = await applicationsApi.createApplication(payload)
          this.formDialogVisible = false
          this.showCredential(result.application, result.secret_key)
          await this.loadApplications(true)
          ElMessage.success('应用已创建')
        }
        this.formDialogVisible = false
      } catch (error) {
        this.handleError(error)
      } finally {
        this.saving = false
      }
    },
    async toggleStatus(application) {
      const status = application.status === 'enabled' ? 'disabled' : 'enabled'
      const successMessage = status === 'enabled' ? '应用已启用；旧访问令牌不会恢复' : '应用已停用；已有访问令牌立即失效'
      await this.runMutation(application, () => applicationsApi.updateApplication(application.id, application.revision, {status}), successMessage)
    },
    async rotateSecret(application) {
      try {
        await ElMessageBox.confirm('轮换后旧 Secret Key 和已签发的访问令牌立即失效。', '确认轮换密钥', {type: 'warning'})
      } catch (error) {
        return
      }
      this.mutatingID = application.id
      try {
        const result = await applicationsApi.rotateSecret(application.id, application.revision)
        this.replaceApplication(result.application)
        this.showCredential(result.application, result.secret_key)
      } catch (error) {
        this.handleError(error)
      } finally {
        this.mutatingID = ''
      }
    },
    async revokeApplication(application) {
      try {
        await ElMessageBox.confirm('永久吊销后不能恢复，旧 Secret Key 和访问令牌立即失效。', '永久吊销应用', {
          type: 'error',
          confirmButtonText: '永久吊销',
          confirmButtonClass: 'el-button--danger',
        })
      } catch (error) {
        return
      }
      await this.runMutation(application, () => applicationsApi.revokeApplication(application.id, application.revision), '应用已永久吊销')
    },
    async runMutation(application, action, successMessage) {
      this.mutatingID = application.id
      try {
        this.replaceApplication(await action())
        ElMessage.success(successMessage)
      } catch (error) {
        this.handleError(error)
      } finally {
        this.mutatingID = ''
      }
    },
    replaceApplication(application) {
      const index = this.applications.findIndex((item) => item.id === application.id)
      if (index >= 0) {
        this.applications.splice(index, 1, application)
      }
    },
    showCredential(application, secretKey) {
      this.clearCredential()
      this.oneTimeCredential = createOneTimeCredential(application, secretKey)
      if (!this.oneTimeCredential) {
        ElMessage.error('服务端未返回一次性 Secret Key，请重新创建或轮换')
        return
      }
      this.secretDialogVisible = true
    },
    async copyCredential() {
      const value = credentialJSON(this.oneTimeCredential)
      if (!value) {
        return
      }
      try {
        await navigator.clipboard.writeText(value)
        ElMessage.success('凭证 JSON 已复制')
      } catch (error) {
        ElMessage.error('复制失败，请手动保存凭证')
      }
    },
    downloadCredential() {
      const value = credentialJSON(this.oneTimeCredential)
      if (!value) {
        return
      }
      const blobURL = URL.createObjectURL(new Blob([value], {type: 'application/json'}))
      const link = document.createElement('a')
      link.href = blobURL
      link.download = `cq-application-${this.oneTimeCredential.applicationID}.json`
      link.click()
      URL.revokeObjectURL(blobURL)
    },
    beforeCredentialClose(done) {
      this.clearCredential()
      done()
    },
    closeCredential() {
      this.clearCredential()
      this.secretDialogVisible = false
    },
    clearCredential() {
      this.oneTimeCredential = clearOneTimeCredential(this.oneTimeCredential)
    },
    scopeText(scope) {
      const option = this.scopeOptions.find((item) => item.name === scope)
      return option ? option.label : scope
    },
    statusTagType(status) {
      return {enabled: 'success', disabled: 'warning', revoked: 'danger'}[status] || 'info'
    },
    expiryText(application) {
      const date = new Date(application.expires_at)
      return Number.isNaN(date.getTime()) ? '到期时间未知' : `到期 ${date.toLocaleDateString('zh-CN')}`
    },
    handleError(error) {
      if (handleIdentityFailure(error, () => {
        this.$store.commit('REMOVE_INFO')
      }, () => {
        this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
      })) {
        return
      }
      if (error.status === 404) {
        ElMessage.error('应用不存在或无权访问')
        return
      }
      if (error.status === 409) {
        ElMessage.error('应用已被其他操作修改，正在刷新最新数据')
        this.formDialogVisible = false
        this.loadApplications(true)
        return
      }
      ElMessage.error(error.message || '应用凭证操作失败')
    },
  },
}
</script>

<style scoped>
.applications-page { max-width: 1200px; }
.applications-heading { align-items: center; margin-bottom: 22px; }
.applications-heading .page-title { font-size: 30px; margin-bottom: 10px; }
.page-desc { color: var(--text-secondary); font-size: 14px; margin: 0; }
.credential-note { margin-bottom: 18px; }
.applications-card { overflow: hidden; padding: 0 24px 10px; }
.list-toolbar { align-items: center; color: var(--text-secondary); display: flex; font-size: 12px; gap: 16px; justify-content: space-between; padding: 20px 0; }
.application-identity { align-items: flex-start; display: flex; gap: 12px; min-width: 0; }
.application-icon { background: #edf4ef; border: 1px solid #dce8df; border-radius: 11px; color: var(--primary-color); display: grid; flex: 0 0 40px; height: 40px; place-items: center; }
.application-text { display: flex; flex-direction: column; min-width: 0; }
.application-text strong { font-size: 14px; overflow-wrap: anywhere; }
.application-text code { color: #526762; font-size: 12px; margin-top: 3px; overflow-wrap: anywhere; }
.application-text small { color: var(--text-secondary); display: -webkit-box; font-size: 12px; margin-top: 5px; overflow: hidden; overflow-wrap: anywhere; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.scope-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.date-text { color: var(--text-secondary); font-size: 12px; margin-top: 6px; }
.row-actions { display: flex; flex-wrap: wrap; gap: 6px; justify-content: flex-end; }
.row-actions :deep(.el-button) { margin-left: 0; }
.revoked-note { color: var(--text-secondary); font-size: 12px; }
.mobile-applications { display: none; }
.mobile-application { border-top: 1px solid var(--border-color); padding: 20px 0; }
.mobile-meta { align-items: center; color: var(--text-secondary); display: flex; font-size: 12px; gap: 12px; justify-content: space-between; margin: 16px 0 12px; }
.mobile-actions { margin-top: 16px; }
.scope-options { display: grid; gap: 10px; grid-template-columns: repeat(2, minmax(0, 1fr)); width: 100%; }
.scope-options :deep(.el-checkbox) { align-items: flex-start; height: auto; margin: 0; min-height: 64px; padding: 10px 12px; width: 100%; }
.scope-options :deep(.el-checkbox__label) { display: flex; flex-direction: column; line-height: 1.4; white-space: normal; }
.scope-options small { color: var(--text-secondary); font-size: 11px; margin-top: 3px; }
.scope-name { font-size: 13px; font-weight: 600; }
.scope-note { margin: -6px 0 20px; }
.field-suffix { color: var(--text-secondary); font-size: 12px; margin-left: 10px; }
.credential-values { margin: 20px 0 0; }
.credential-values > div { margin-top: 14px; }
.credential-values dt { color: var(--text-secondary); font-size: 12px; font-weight: 600; }
.credential-values dd { background: #f4f7f4; border: 1px solid var(--border-color); border-radius: 8px; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; margin: 6px 0 0; overflow-wrap: anywhere; padding: 12px; user-select: all; }
.load-more { padding: 20px 0; text-align: center; }
@media (max-width: 820px) {
  .desktop-applications { display: none; }
  .mobile-applications { display: block; }
  .applications-card { padding: 0 18px; }
}
@media (max-width: 640px) {
  .applications-heading { align-items: flex-start; }
  .applications-heading .page-title { font-size: 26px; }
  .scope-options { grid-template-columns: 1fr; }
}
@media (max-width: 480px) {
  .applications-heading { flex-direction: column; }
  .applications-heading > .el-button { width: 100%; }
  .list-toolbar > span { max-width: 220px; }
}
</style>
