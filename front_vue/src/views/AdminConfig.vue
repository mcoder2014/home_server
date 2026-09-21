<template>
  <div class="page-toolbar"><div><h1 class="page-title">站点设置</h1><p class="muted">分组修改、校验差异后发布；已保存版本与实际生效版本分别展示。</p></div><el-button :loading="statusLoading" @click="refreshStatus">刷新生效状态</el-button></div>
  <el-alert v-if="error" type="error" :title="error" :closable="false" class="form-message" />
  <section class="surface-card section-card" v-if="status">
    <div class="runtime-summary"><div><el-tag :type="status.apply_state === 'applied' ? 'success' : 'warning'">{{ applyText[status.apply_state] || '生效状态待确认' }}</el-tag><span class="muted"> 最近刷新：{{ dateTime(status.last_refresh_at) }}（UTC+8）</span></div><span class="muted">已保存代次 {{ status.persisted_generation }} · 已加载代次 {{ status.loaded_generation }}</span></div>
    <el-alert v-if="status.last_error" type="warning" :title="`配置已保存，但运行加载存在错误：${status.last_error}`" :closable="false" class="form-message" />
    <p v-if="status.apply_state !== 'applied'" class="muted">请查看各组生效版本。页面每 15 秒刷新状态；发布成功不代表所有运行参数已经加载。</p>
  </section>
  <el-skeleton v-if="loading" :rows="8" animated />
  <template v-else-if="schemas.length">
    <el-select :model-value="namespace" class="namespace-select" @change="selectNamespace"><el-option v-for="group in schemas" :key="group.namespace" :label="group.label" :value="group.namespace" /></el-select>
    <section v-if="current && schema" class="surface-card section-card">
      <div class="page-toolbar"><div><h2>{{ schema.label }}</h2><div class="muted">保存版本 {{ current.revision }} · 生效版本 {{ namespaceStatus?.loaded_revision ?? current.loaded_revision ?? '—' }} · {{ applyText[namespaceStatus?.apply_state || current.apply_state] || '待确认' }}</div><div class="muted">最后修改：{{ current.updated_by || '初始化' }} · {{ dateTime(current.update_time) }}</div></div><el-button @click="loadHistory(true)">版本历史</el-button></div>
      <el-alert v-if="conflict" type="warning" title="配置已被其他管理员修改，当前草稿已保留" description="请查看最新版本，确认差异后再继续发布。" :closable="false" class="form-message"><el-button size="small" @click="loadRemote">查看最新版本</el-button></el-alert>
      <el-form label-position="top" class="config-form" @submit.prevent="validateDraft">
        <div v-for="field in schema.fields" :key="field.key" class="config-field">
          <el-form-item :label="field.label + (field.unit ? `（${field.unit}）` : '')">
            <div v-if="field.read_only" class="readonly-value">{{ formatValue(field.default_value) }} <el-tag type="info" size="small">固定规则</el-tag></div>
            <el-switch v-else-if="field.type === 'boolean'" v-model="draft[field.key]" active-text="开启" inactive-text="关闭" :disabled="saving" @change="invalidateValidation" />
            <el-input-number v-else-if="field.type === 'integer'" v-model="draft[field.key]" :min="field.minimum" :max="field.maximum" :precision="0" :step="1" controls-position="right" :disabled="saving" @change="invalidateValidation" />
            <el-input v-else v-model="draft[field.key]" :type="field.key.includes('notice') ? 'textarea' : 'text'" :rows="4" :maxlength="field.max_length" show-word-limit :disabled="saving" @input="invalidateValidation" />
            <div class="field-help muted"><span v-if="!field.read_only">{{ effectText[field.effect] || '按服务端运行策略生效' }}</span><span v-if="field.minimum != null || field.maximum != null"> · 范围 {{ field.minimum ?? '不限' }}–{{ field.maximum ?? '不限' }}</span><span v-if="field.public"> · 公开展示字段</span></div>
          </el-form-item>
        </div>
        <p v-if="namespace === 'registration'" class="danger-note">关闭注册后，不能生成或兑换邀请码，现有未使用邀请码永久失效。重新开启、版本回滚均不会复活旧邀请码；管理员仍可手动添加用户。</p>
        <p v-if="namespace === 'webdav'" class="danger-note">全站开关与个人授权共同生效。WebDAV 使用共享根目录，打开站点能力不会自动授权任何账号。</p>
        <p v-if="namespace === 'library'" class="muted">全站开关与个人授权共同生效，打开站点能力不会自动授予个人藏书权限。</p>
        <p v-if="namespace === 'account_policy'" class="muted">密码长度仅约束新建或修改密码，已有密码和分享码仍可使用。账号密码按字符计数，分享密码按 UTF-8 字节计数（ASCII 字符各占 1 字节），上限均受 72 字节限制。网站登录上限按有效会话计数，不代表物理设备数。达到上限会拒绝新的登录；降低上限不会主动退出已有会话，超额会话保留至主动退出或过期。</p>
        <div class="action-row"><el-button type="primary" native-type="submit" :loading="validating" :disabled="!dirty || conflict || saving">校验并查看差异</el-button><el-button :disabled="!dirty || saving" @click="resetDraft">撤销草稿</el-button></div>
      </el-form>
    </section>
  </template>
  <AdminConfirm v-model="confirmVisible" :title="rollbackTarget ? '回滚配置' : '发布配置'" :description="confirmationDescription" :loading="saving" :server-error="publishError" @confirm="publish">
    <el-table :data="validation?.changes || []" size="small"><el-table-column label="配置项" min-width="145"><template #default="{row}">{{ fieldLabel(row.key) }}</template></el-table-column><el-table-column label="变更前" min-width="120"><template #default="{row}">{{ formatValue(row.before) }}</template></el-table-column><el-table-column label="变更后" min-width="120"><template #default="{row}">{{ formatValue(row.after) }}</template></el-table-column></el-table>
    <p class="muted">影响：{{ (validation?.effects || []).map(effect => effectText[effect] || effect).join('；') }}</p>
  </AdminConfirm>
  <el-drawer v-model="historyVisible" :title="`${schema?.label || ''} · 版本历史`" size="min(850px, 96vw)"><el-alert v-if="historyError" type="error" :title="historyError" :closable="false" class="form-message" /><el-table :data="history" v-loading="historyLoading" empty-text="暂无历史版本"><el-table-column type="expand"><template #default="{row}"><el-table :data="historyFields(row)" size="small"><el-table-column prop="label" label="配置项" /><el-table-column prop="value" label="值" /></el-table></template></el-table-column><el-table-column prop="revision" label="版本" width="80" /><el-table-column prop="actor_user_id" label="操作者 ID" min-width="120" /><el-table-column label="时间" min-width="175"><template #default="{row}">{{ dateTime(row.create_time) }}</template></el-table-column><el-table-column prop="reason" label="原因" min-width="150" /><el-table-column label="操作" width="85"><template #default="{row}"><el-button type="primary" link :disabled="String(row.revision) === String(current?.revision) || validating || saving" @click="prepareRollback(row)">回滚</el-button></template></el-table-column></el-table><div class="data-footer" v-if="historyMore"><el-button :loading="historyLoading" @click="loadHistory(false)">加载更多</el-button></div><p class="muted">回滚会创建新版本，不删除历史，也不恢复旧邀请码、旧权限、已删除内容或过期凭证。</p></el-drawer>
  <el-dialog v-model="remoteVisible" title="最新版本与草稿" width="700px"><p class="muted">服务器已更新至版本 {{ remote?.revision }}。下面展示你的草稿与服务器最新值的差异。</p><el-table :data="remoteChanges"><el-table-column label="配置项"><template #default="{row}">{{ fieldLabel(row.key) }}</template></el-table-column><el-table-column label="服务器最新值"><template #default="{row}">{{ formatValue(row.before) }}</template></el-table-column><el-table-column label="当前草稿"><template #default="{row}">{{ formatValue(row.after) }}</template></el-table-column></el-table><template #footer><el-button @click="useRemote(false)">使用最新版本</el-button><el-button type="primary" @click="useRemote(true)">保留草稿，重新校验</el-button></template></el-dialog>
</template>
<script>
import AdminConfirm from '@/components/AdminConfirm.vue'
const {accountsApi} = require('@/api/accounts.cjs')
const {configValues, configChanges, dateTime, requestID} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'AdminConfig', components: {AdminConfirm},
  data() { return {schemas: [], records: [], namespace: '', draft: {}, loading: true, error: '', status: null, statusLoading: false, timer: null, conflict: false, remote: null, remoteVisible: false, validating: false, validation: null, confirmVisible: false, saving: false, publishError: '', pendingRequest: null, rollbackTarget: null, history: [], historyVisible: false, historyLoading: false, historyError: '', historyCursor: '', historyMore: false,
    applyText: {applied: '已生效', pending: '已保存，等待生效', stale: '运行配置尚未更新'}, effectText: {immediate: '下一次操作检查时生效', new_operation: '对新操作生效，已有数据保留原语义', refresh: '配置刷新后生效'},
  } },
  computed: {
    schema() { return this.schemas.find(item => item.namespace === this.namespace) },
    current() { return this.records.find(item => item.namespace === this.namespace) },
    namespaceStatus() { return this.status?.namespaces?.find(item => item.namespace === this.namespace) },
    dirty() { return !!this.current && configChanges(this.current.values, this.draft).length > 0 },
    remoteChanges() { return this.remote ? configChanges(this.remote.values, this.draft) : [] },
    confirmationDescription() { return this.namespace === 'registration' && this.validation?.values?.enabled === false ? '关闭注册会永久撤销现有未使用邀请码，重新开启或回滚均不会复活。管理员仍可直接建号。' : this.rollbackTarget ? `将历史版本 ${this.rollbackTarget.revision} 发布为新版本。现有草稿会被替换；业务数据和旧凭证不会恢复。` : '按本组当前版本原子发布，写入版本历史与审计。发布后请核对实际生效版本。' },
  },
  async created() {
    try { const [schema, records] = await Promise.all([accountsApi.configSchema(), accountsApi.configs()]); this.schemas = schema.namespaces; this.records = records.items; this.namespace = this.schemas[0]?.namespace || ''; this.resetDraft(); await this.refreshStatus() }
    catch (error) { this.error = error.message }
    finally { this.loading = false }
    this.timer = setInterval(() => this.refreshStatus(), 15000)
  },
  mounted() { window.addEventListener('beforeunload', this.beforeUnload) },
  beforeUnmount() { clearInterval(this.timer); window.removeEventListener('beforeunload', this.beforeUnload); this.pendingRequest = null },
  async beforeRouteLeave() { if (!this.dirty) return true; return this.confirmDiscard() },
  methods: {
    dateTime,
    formatValue(value) { return typeof value === 'boolean' ? value ? '开启' : '关闭' : value == null || value === '' ? '（空）' : String(value) },
    fieldLabel(key) { return this.schema?.fields.find(field => field.key === key)?.label || key },
    historyFields(record) { return Object.entries(record.values).map(([key, value]) => ({label: this.fieldLabel(key), value: this.formatValue(value)})) },
    beforeUnload(event) { if (this.dirty) { event.preventDefault(); event.returnValue = '' } },
    async confirmDiscard() { try { await this.$confirm('当前配置草稿尚未发布，离开会丢失修改。', '未发布的配置', {confirmButtonText: '放弃草稿', cancelButtonText: '继续编辑', type: 'warning'}); return true } catch (_) { return false } },
    async selectNamespace(namespace) { if (this.dirty && !await this.confirmDiscard()) return; this.namespace = namespace; this.resetDraft(); this.error = '' },
    resetDraft() { this.draft = {...(this.current?.values || {})}; this.validation = null; this.pendingRequest = null; this.conflict = false; this.rollbackTarget = null },
    invalidateValidation() { this.validation = null; this.pendingRequest = null; this.rollbackTarget = null },
    async refreshStatus() { if (this.statusLoading) return; this.statusLoading = true; try { this.status = await accountsApi.configStatus() } catch (error) { this.error = `无法获取最新生效状态：${error.message}` } finally { this.statusLoading = false } },
    async validateDraft() {
      this.error = ''; this.validating = true; this.rollbackTarget = null
      try {
        const values = configValues(this.schema, this.draft)
        this.validation = await accountsApi.validateConfig(this.namespace, values)
        if (String(this.validation.revision) !== String(this.current.revision)) { this.conflict = true; return }
        this.publishError = ''; this.confirmVisible = true
      } catch (error) { this.error = error.message }
      finally { this.validating = false }
    },
    async publish(confirmation) {
      if (this.saving || !this.validation) return
      this.saving = true; this.publishError = ''
      if (!this.pendingRequest) this.pendingRequest = {request_id: requestID(), reason: confirmation.reason, ...(this.rollbackTarget ? {target_revision: this.rollbackTarget.revision} : {values: {...this.validation.values}})}
      try {
        const data = {...this.pendingRequest, current_password: confirmation.current_password}
        const record = this.rollbackTarget ? await accountsApi.rollbackConfig(this.namespace, this.current.revision, data) : await accountsApi.publishConfig(this.namespace, this.current.revision, data)
        this.records = this.records.map(item => item.namespace === record.namespace ? record : item)
        this.confirmVisible = false; this.historyVisible = false; this.resetDraft(); this.$message.success(`配置版本 ${record.revision} 已保存，请核对生效状态`)
        await this.refreshStatus()
        try { await this.$store.dispatch('loadBootstrap') } catch (_) { /* The saved version and runtime state remain visible. */ }
      } catch (error) {
        this.publishError = error.status === 409 ? '版本发生冲突，草稿已保留。关闭弹窗后查看最新版本。' : error.message
        if (error.status === 409) this.conflict = true
        if (error.status && error.status < 500) this.pendingRequest = null
      } finally { this.saving = false }
    },
    async loadHistory(reset) { this.historyVisible = true; this.historyLoading = true; this.historyError = ''; try { const result = await accountsApi.configHistory(this.namespace, {cursor: reset ? '' : this.historyCursor, limit: 20}); this.history = reset ? result.items : this.history.concat(result.items); this.historyCursor = result.next_cursor || ''; this.historyMore = !!result.has_more } catch (error) { this.historyError = error.message } finally { this.historyLoading = false } },
    async prepareRollback(record) {
      this.validating = true; this.historyError = ''
      try {
        const candidate = {...record.values}
        if (this.namespace === 'account_policy' && !Object.prototype.hasOwnProperty.call(candidate, 'max_active_sessions')) {
          const field = this.schema.fields.find(field => field.key === 'max_active_sessions')
          if (field) candidate.max_active_sessions = field.default_value
        }
        const values = configValues(this.schema, candidate)
        this.validation = await accountsApi.validateConfig(this.namespace, values)
        if (String(this.validation.revision) !== String(this.current.revision)) { this.conflict = true; this.historyVisible = false; return }
        this.rollbackTarget = record; this.pendingRequest = null; this.publishError = ''; this.confirmVisible = true
      }
      catch (error) { this.historyError = error.message }
      finally { this.validating = false }
    },
    async loadRemote() { try { this.remote = await accountsApi.config(this.namespace); this.remoteVisible = true } catch (error) { this.error = error.message } },
    useRemote(keepDraft) { this.records = this.records.map(item => item.namespace === this.namespace ? this.remote : item); if (!keepDraft) this.resetDraft(); this.conflict = false; this.pendingRequest = null; this.validation = null; this.remoteVisible = false },
  },
}
</script>
<style scoped>.runtime-summary{display:flex;flex-direction:column;gap:12px}.namespace-select{width:100%;margin-bottom:20px}.config-form{max-width:700px}.config-field{padding:12px 0;border-bottom:1px solid var(--border-color)}.config-field:last-of-type{margin-bottom:20px}.field-help{width:100%;font-size:12px;margin-top:8px}.readonly-value{display:flex;gap:10px;align-items:center}.config-form .el-input-number{width:230px}</style>
