<template>
  <div class="page-toolbar"><div><h1 class="page-title">home virtual sub</h1><p class="muted">管理订阅配置与运行状态，页面每 5 秒刷新状态。</p></div><div class="action-row"><el-button :loading="loading" @click="refreshStatus">刷新</el-button><el-button type="primary" :disabled="!canManage || saving" @click="openCreate">新增订阅</el-button></div></div>
  <el-alert v-if="error" type="error" :title="error" :closable="false" class="form-message" />
  <el-alert v-if="notice" :type="noticeType" :title="notice" :closable="false" class="form-message" />
  <el-alert v-if="status && !status.enabled" type="info" title="管理连接尚未启用" description="部署管理员配置 home virtual sub 的本机连接后，即可管理订阅。" :closable="false" class="form-message" />
  <section class="surface-card section-card">
    <div class="status-summary"><el-tag :type="canManage ? 'success' : 'info'">{{ status ? status.enabled ? '服务已连接' : '未启用' : '当前状态未知' }}</el-tag><template v-if="canManage"><span>订阅 {{ status.total }}</span><span>运行 {{ status.running }}</span><span>停止 {{ status.stopped }}</span><span class="muted">核心 {{ status.core_type }} · 检查间隔 {{ status.interval_second }} 秒</span></template></div>
    <p v-if="canManage" class="muted">服务启动：{{ dateTime(status.started_at) }} · 最近读取：{{ dateTime(lastReadAt) }}（UTC+8）</p>
    <p v-else class="muted">连接失败时，旧运行状态不作为在线依据。已有配置需重新连接后才能修改。</p>
  </section>
  <section v-if="subscriptions.length || canManage" class="surface-card section-card">
    <el-alert v-if="listError" type="warning" :title="listError" :closable="false" class="form-message" />
    <el-table :data="subscriptions" empty-text="没有订阅，可点击新增订阅" style="width:100%">
      <el-table-column prop="name" label="订阅" min-width="150" />
      <el-table-column label="监听" min-width="180"><template #default="{row}">{{ row.bind_ip }}<div class="muted">HTTP {{ row.http_port }} · SOCKS {{ row.socket_port }}</div></template></el-table-column>
      <el-table-column label="配置" width="105"><template #default="{row}">{{ row.url_count }} 个地址<div class="muted">{{ row.outbound_server_count }} 个出站</div></template></el-table-column>
      <el-table-column label="状态" min-width="175"><template #default="{row}"><el-tag :type="presentation(row.name).type">{{ presentation(row.name).label }}</el-tag><div v-if="worker(row.name)?.last_error" class="muted">{{ worker(row.name).last_error }}</div><div class="muted">最近成功：{{ dateTime(worker(row.name)?.last_success_at) }}</div></template></el-table-column>
      <el-table-column label="操作" min-width="250"><template #default="{row}"><div class="row-actions"><el-button size="small" :disabled="!canManage || saving" @click="openEdit(row)">编辑</el-button><el-button size="small" :disabled="!canManage || saving" @click="prepareAction('refresh', row)">刷新订阅</el-button><el-button size="small" :disabled="!canManage || saving" @click="prepareAction(worker(row.name)?.enabled ? 'stop' : 'start', row)">{{ worker(row.name)?.enabled ? '停止' : '启动' }}</el-button><el-button size="small" type="danger" plain :disabled="!canManage || saving" @click="prepareAction('delete', row)">删除</el-button></div></template></el-table-column>
    </el-table>
    <p class="muted">停止是临时操作，服务重启后按原有规则启动。删除会永久移除配置，并停止对应代理。</p>
  </section>
  <el-dialog v-model="editorVisible" :title="editing ? '编辑订阅' : '新增订阅'" width="min(620px, 94vw)" :close-on-click-modal="false" :close-on-press-escape="!saving" @close="clearDraft">
    <el-alert v-if="editorError" type="error" :title="editorError" :closable="false" class="form-message" />
    <el-alert v-if="conflict" type="warning" title="配置版本已变化，草稿已保留" description="重新读取配置会替换当前草稿，请核对后再编辑。" :closable="false" class="form-message"><el-button :disabled="saving" @click="reloadDraft">重新读取配置</el-button></el-alert>
    <el-alert v-if="hasProxyChain" type="info" title="此订阅已有代理链，编辑会保留该配置" :closable="false" class="form-message" />
    <el-form label-position="top" @submit.prevent="prepareSave">
      <el-form-item label="订阅名称"><el-input v-model="draft.name" :disabled="editing || saving" autocomplete="off" maxlength="256" /></el-form-item>
      <el-form-item label="订阅地址（每行一个）"><el-input v-model="draft.urls_text" type="textarea" :rows="5" :disabled="saving" autocomplete="off" placeholder="https://example.com/subscription" /></el-form-item>
      <el-form-item label="监听 IP"><el-input v-model="draft.bind_ip" :disabled="saving" placeholder="127.0.0.1" /></el-form-item>
      <p class="muted">运行中扩大监听范围（如 127.0.0.1 改为 0.0.0.0）并保留端口时，请先停止订阅，再修改并启动。</p>
      <div class="port-fields"><el-form-item label="HTTP 端口"><el-input-number v-model="draft.http_port" :min="1" :max="65535" :precision="0" :disabled="saving" /></el-form-item><el-form-item label="SOCKS 端口"><el-input-number v-model="draft.socket_port" :min="1" :max="65535" :precision="0" :disabled="saving" /></el-form-item><el-form-item label="出站节点数量"><el-input-number v-model="draft.outbound_server_count" :min="1" :max="5" :precision="0" :disabled="saving" /></el-form-item></div>
      <p class="muted">订阅地址可能包含凭据，请勿公开分享。修改配置会刷新此订阅，变更端口时对应代理可能短暂中断。</p>
      <div class="action-row"><el-button :disabled="saving" @click="editorVisible = false; clearDraft()">取消</el-button><el-button type="primary" native-type="submit" :disabled="!canManage || conflict || saving">确认配置</el-button></div>
    </el-form>
  </el-dialog>
  <AdminConfirm v-model="confirmVisible" :title="confirmTitle" :description="confirmDescription" :loading="saving" :server-error="confirmError" @confirm="submit" @update:model-value="closeConfirmation">
    <p v-if="pendingAction">订阅：<strong>{{ pendingAction.name }}</strong></p>
  </AdminConfirm>
</template>
<script>
import AdminConfirm from '@/components/AdminConfirm'
const {homeVirtualSubApi: api} = require('@/api/home_virtual_sub.cjs')
const {subscriptionInput, createStatusLoader, workerPresentation} = require('@/utils/home_virtual_sub_behavior.cjs')
const actionText = {create: '新增订阅', update: '保存订阅', delete: '删除订阅', refresh: '刷新订阅', start: '启动订阅', stop: '停止订阅'}

export default {
  name: 'AdminHomeVirtualSub', components: {AdminConfirm},
  data() {
    return {status: null, subscriptions: [], revision: '', error: '', listError: '', notice: '', noticeType: 'success', lastReadAt: '', loading: false, listLoading: false,
      editorVisible: false, editing: false, editEpoch: 0, draft: {name: '', urls_text: '', bind_ip: '127.0.0.1', http_port: null, socket_port: null, outbound_server_count: 1}, draftRevision: '', hasProxyChain: false, editorError: '', conflict: false,
      pendingAction: null, confirmVisible: false, confirmError: '', saving: false, mutationUncertain: false, listEpoch: 0, loader: null, timer: null, disposed: false}
  },
  computed: {
    canManage() { return !!this.status?.enabled && !this.mutationUncertain },
    confirmTitle() { return actionText[this.pendingAction?.action] || '订阅操作' },
    confirmDescription() {
      return this.pendingAction?.action === 'delete' ? '删除将停止该订阅的代理并永久移除配置，请确认影响。' : this.pendingAction?.action === 'stop' ? '停止后定时刷新不会重新启动；服务重启后恢复原有启动规则。' : '操作只影响此订阅。配置保存与运行生效分别报告，请在完成后核对状态。'
    },
  },
  mounted() {
    this.loader = createStatusLoader(() => api.status(), value => {
      this.status = value
      if (value) {
        this.lastReadAt = new Date().toISOString()
        if (value.enabled && value.revision !== this.revision) this.loadList()
        if (!value.enabled) { this.subscriptions = []; this.revision = '' }
      }
    }, error => { this.error = error ? error.message : '' })
    document.addEventListener('visibilitychange', this.visibilityChanged)
    this.visibilityChanged()
    this.timer = setInterval(() => { if (!document.hidden) this.refreshStatus() }, 5000)
  },
  beforeUnmount() {
    this.disposed = true
    clearInterval(this.timer)
    document.removeEventListener('visibilitychange', this.visibilityChanged)
    this.loader?.deactivate()
    this.clearDraft()
    this.pendingAction = null
  },
  methods: {
    visibilityChanged() {
      if (document.hidden) this.loader?.deactivate()
      else { this.loader?.activate(); this.refreshStatus() }
    },
    async refreshStatus() {
      if (!this.loader) return
      this.loading = true
      try {
        const uncertain = this.mutationUncertain
        const checked = uncertain ? await this.loadList() : false
        await this.loader.refresh()
        if (uncertain && checked && this.status?.enabled && this.status.revision === this.revision) {
          this.mutationUncertain = false
          this.notice = '已重新读取配置和状态，请核对订阅变更后再操作'; this.noticeType = 'warning'
        }
      } finally { this.loading = false }
    },
    async loadList() {
      if (this.listLoading || this.disposed) return false
      const epoch = this.listEpoch
      this.listLoading = true
      try {
        const result = await api.list()
        if (this.disposed || epoch !== this.listEpoch) return false
        this.subscriptions = result.subscriptions || []
        this.revision = result.revision
        this.listError = ''
        return true
      } catch (error) { if (!this.disposed && epoch === this.listEpoch) { this.listError = error.message; this.revision = '' }; return false }
      finally { this.listLoading = false }
    },
    worker(name) { return this.status?.workers?.find(worker => worker.name === name) },
    presentation(name) { return workerPresentation(this.worker(name)) },
    dateTime(value) { return value ? new Date(value).toLocaleString('zh-CN', {timeZone: 'Asia/Shanghai', hour12: false}) : '—' },
    clearDraft() {
      this.editEpoch++
      this.draft = {name: '', urls_text: '', bind_ip: '127.0.0.1', http_port: null, socket_port: null, outbound_server_count: 1}
      this.draftRevision = ''; this.hasProxyChain = false; this.editorError = ''; this.conflict = false
    },
    openCreate() {
      this.clearDraft()
      this.editing = false; this.draftRevision = this.revision; this.editorVisible = true
    },
    async openEdit(row) {
      const epoch = ++this.editEpoch
      this.editorError = ''
      try {
        const result = await api.detail(row.name)
        if (this.disposed || epoch !== this.editEpoch) return
        const sub = result.subscription
        this.draft = {name: sub.name, urls_text: (sub.urls || []).join('\n'), bind_ip: sub.bind_ip, http_port: sub.http_port, socket_port: sub.socket_port, outbound_server_count: sub.outbound_server_count}
        this.draftRevision = result.revision; this.hasProxyChain = sub.has_proxy_chain; this.editing = true; this.conflict = false; this.editorVisible = true
      } catch (error) { if (!this.disposed && epoch === this.editEpoch) this.error = error.message }
    },
    async reloadDraft() {
      if (this.editing) await this.openEdit({name: this.draft.name})
      else {
        const epoch = this.editEpoch
        await this.loadList()
        if (this.disposed || epoch !== this.editEpoch || !this.editorVisible) return
        this.draftRevision = this.revision; this.conflict = false
      }
    },
    prepareSave() {
      try {
        const input = subscriptionInput(this.draft)
        if (!this.draftRevision) throw new Error('请先刷新配置')
        this.pendingAction = {action: this.editing ? 'update' : 'create', name: input.name, revision: this.draftRevision, input}
        this.confirmError = ''; this.editorError = ''; this.confirmVisible = true
      } catch (error) { this.editorError = error.message }
    },
    prepareAction(action, row) {
      if (!this.revision) { this.error = '请先刷新配置'; this.loadList(); return }
      this.pendingAction = {action, name: row.name, revision: this.revision, input: {}}
      this.confirmError = ''; this.confirmVisible = true
    },
    closeConfirmation(visible) { if (!visible && !this.saving) this.pendingAction = null },
    async submit(confirmation) {
      if (this.saving || !this.pendingAction) return
      if (!this.canManage) { this.confirmError = '当前服务状态未知，请刷新后核对'; return }
      this.saving = true; this.confirmError = ''
      const pending = this.pendingAction, body = {...pending.input, ...confirmation}
      try {
        const result = pending.action === 'create' ? await api.create(pending.revision, body) : pending.action === 'update' ? await api.update(pending.name, pending.revision, body) : pending.action === 'delete' ? await api.remove(pending.name, pending.revision, body) : await api.action(pending.action, pending.name, pending.revision, body)
        this.notice = result.message; this.noticeType = result.apply_state === 'error' || result.audit_recorded === false ? 'warning' : 'success'
        this.confirmVisible = false; this.pendingAction = null; this.editorVisible = false; this.mutationUncertain = false
        await this.loadList(); await this.refreshStatus()
      } catch (error) {
        this.confirmError = error.message
        if (error.status === 409) { this.conflict = true; this.editorError = error.message; this.notice = error.message; this.noticeType = 'warning'; this.confirmVisible = false; this.pendingAction = null; await this.loadList() }
        else if (!error.status || error.status >= 500) { this.loader?.invalidate(); this.listEpoch++; this.status = null; this.mutationUncertain = true; this.notice = error.message + '；请刷新配置和状态核对后再操作'; this.noticeType = 'warning'; this.confirmVisible = false; this.pendingAction = null }
      } finally { this.saving = false }
    },
  },
}
</script>
<style scoped>
.section-card{padding:22px;margin-bottom:20px}.status-summary,.row-actions{display:flex;align-items:center;gap:12px;flex-wrap:wrap}.row-actions{gap:6px}.row-actions .el-button{margin-left:0}.port-fields{display:flex;gap:18px;flex-wrap:wrap}.status-summary span{font-size:14px}.muted{margin-top:8px;font-size:12px}.port-fields .el-input-number{width:160px}@media(max-width:700px){.page-toolbar{align-items:flex-start;flex-wrap:wrap}.section-card{padding:14px}}
</style>
