<template>
  <section class="login-sessions">
    <div class="page-toolbar"><div><h2>{{ administrator ? '有效登录会话' : '登录管理' }}</h2><p class="muted">{{ loaded ? `有效登录会话 ${total} 个` : '正在获取有效登录会话数量' }}<span v-if="loaded && restricted">，其中 {{ restricted }} 个仅可改密</span>。会话数量不代表物理设备数，也不代表正在在线。</p></div><el-button :loading="loading" :disabled="acting" @click="load(true)">刷新</el-button></div>
    <p v-if="loaded && maxActiveSessions" class="muted">本网站每个账号的有效登录上限为 {{ maxActiveSessions }} 个会话，当前 {{ total }} / {{ maxActiveSessions }}。达到上限时会拒绝新的登录；已存在的超额会话保留，可主动退出或等待过期。</p>
    <p v-if="lastUpdated" class="muted">数据截至 {{ dateTime(lastUpdated) }}（UTC+8）</p>
    <el-alert v-if="error" :title="loaded ? '刷新失败，保留上次确认的数据' : '暂时无法获取登录会话'" :description="error" type="error" :closable="false" show-icon class="form-message" />
    <el-skeleton v-if="loading && !loaded" animated :rows="5" />
    <template v-if="loaded">
      <section v-if="!administrator" class="surface-card session-card current-session"><div class="session-top"><h3>当前登录</h3><el-tag type="success">当前会话</el-tag></div><template v-if="current"><div class="session-client">{{ clientText(current) }}</div><SessionDetails :session="current" :remaining="remaining(current)" /><el-button type="danger" plain :loading="acting" @click="logoutCurrent">退出当前登录</el-button></template><p v-else class="muted">服务端未返回当前会话，请刷新确认。</p></section>
      <div class="session-actions" v-if="!administrator"><h3>其他有效登录</h3><div class="action-row"><el-checkbox :model-value="allSelected" :indeterminate="selectedIDs.length > 0 && !allSelected" :disabled="!items.length || acting || loading" @change="selectAll">选择已加载会话</el-checkbox><el-button type="danger" plain :disabled="!selectedIDs.length || loading" :loading="acting" @click="revokeSelected">退出选中 {{ selectedIDs.length || '' }}</el-button><el-button type="danger" :disabled="total <= (current ? 1 : 0) || loading" :loading="acting" @click="revokeOthers">退出其他全部登录</el-button></div></div>
      <p v-if="!items.length" class="session-empty muted">{{ administrator ? '暂无有效登录' : '暂无其他有效登录' }}</p>
      <div class="session-list"><article v-for="session in items" :key="session.id" class="surface-card session-card"><div class="session-top"><el-checkbox v-if="!administrator" :model-value="selected.includes(String(session.id))" :disabled="acting || loading || remaining(session) === 0" :aria-label="'选择 ' + clientText(session)" @change="value => selectOne(session, value)" /><h3>{{ clientText(session) }}</h3><el-tag v-if="session.purpose === 'password_change'" type="warning">仅可改密</el-tag></div><SessionDetails :session="session" :remaining="remaining(session)" /></article></div>
      <div v-if="hasMore" class="data-footer"><el-button :loading="loading" :disabled="acting" @click="load(false)">加载更多（每页 20 条）</el-button></div>
      <p class="muted">登录 IP 和客户端来自登录时记录，UA 可被伪造。日期均为 UTC+8；剩余时间按服务端时间计算。</p>
    </template>
  </section>
</template>
<script>
import SessionDetails from '@/components/SessionDetails.vue'
const {accountsApi} = require('@/api/accounts.cjs')
const {dateTime, selectedSessionIDs, sessionRemaining} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'LoginSessions', components: {SessionDetails},
  props: {administrator: Boolean, userId: {type: String, default: ''}},
  data() { return {current: null, items: [], selected: [], total: 0, restricted: 0, maxActiveSessions: null, loaded: false, loading: false, acting: false, error: '', cursor: '', hasMore: false, lastUpdated: '', receivedAt: 0, now: Date.now(), timer: null, expiryRefreshed: false, sequence: 0} },
  computed: {
    selectedIDs() { return selectedSessionIDs(this.selected, this.items, this.current) },
    allSelected() { return this.items.length > 0 && this.selectedIDs.length === this.items.filter(session => this.remaining(session) !== 0).length && this.selectedIDs.length > 0 },
  },
  created() { this.load(true) },
  mounted() { this.timer = setInterval(this.tick, 10000) },
  beforeUnmount() { this.sequence++; clearInterval(this.timer) },
  watch: {userId() { this.sequence++; this.current = null; this.items = []; this.selected = []; this.loaded = false; this.lastUpdated = ''; this.loading = false; this.load(true) }},
  methods: {
    dateTime,
    clientText(session) { return [session.client_name || '未知客户端', session.os_name || '未知系统', {desktop: '桌面', mobile: '手机', tablet: '平板', bot: '自动客户端'}[session.device_type] || '设备类型未知'].join(' · ') },
    remaining(session) { return sessionRemaining(session, this.receivedAt, this.now) },
    tick() {
      this.now = Date.now()
      if (!this.loaded || this.loading || this.acting || this.expiryRefreshed) return
      if ([this.current, ...this.items].filter(Boolean).some(session => this.remaining(session) === 0)) { this.expiryRefreshed = true; this.load(true) }
    },
    async load(reset) {
      if (this.loading) return
      const sequence = ++this.sequence
      this.loading = true; this.error = ''
      try {
        const query = {cursor: reset ? '' : this.cursor, limit: 20}
        const data = this.administrator ? await accountsApi.userSessions(this.userId, query) : await accountsApi.sessions(query)
        if (sequence !== this.sequence) return
        if (data.has_more && (!data.next_cursor || (!reset && data.next_cursor === this.cursor))) throw new Error('分页游标异常，请刷新')
        const receivedAt = Date.now()
        // 已加载会话的 remaining_seconds 也须推进到本次接收时刻，避免加载下一页延长旧会话倒计时。
        const oldItems = reset ? [] : this.items.map(session => ({...session, remaining_seconds: sessionRemaining(session, this.receivedAt, receivedAt)}))
        const merged = new Map(oldItems.concat(data.items || []).map(session => [String(session.id), session]))
        this.items = [...merged.values()]; this.current = data.current_session || null
        this.total = data.total_count; this.restricted = data.restricted_count || 0
        this.maxActiveSessions = Number.isInteger(data.max_active_sessions) && data.max_active_sessions >= 1 && data.max_active_sessions <= 100 ? data.max_active_sessions : null
        this.cursor = data.next_cursor || ''; this.hasMore = !!data.has_more; this.lastUpdated = data.server_time
        this.receivedAt = this.now = receivedAt; this.loaded = true; this.expiryRefreshed = false
        this.selected = selectedSessionIDs(this.selected, this.items, this.current)
      } catch (error) { if (sequence === this.sequence) this.error = error.message || '网络连接失败' }
      finally { if (sequence === this.sequence) this.loading = false }
    },
    selectAll(value) { this.selected = value ? this.items.filter(session => this.remaining(session) !== 0).map(session => String(session.id)).slice(0, 100) : [] },
    selectOne(session, value) {
      const id = String(session.id)
      if (!value) this.selected = this.selected.filter(selected => selected !== id)
      else if (!this.selected.includes(id)) {
        if (this.selected.length >= 100) { this.$message.warning('一次最多选择 100 条会话'); return }
        this.selected.push(id)
      }
    },
    async revokeSelected() {
      const ids = this.selectedIDs
      if (this.administrator || this.acting || !ids.length) return
      const labels = this.items.filter(session => ids.includes(String(session.id))).slice(0, 3).map(this.clientText).join('、')
      try { await this.$confirm(`退出选中的 ${ids.length} 个登录会话（${labels}）？当前登录和应用凭证保持有效。`, '退出选中登录', {type: 'warning', confirmButtonText: '确认退出选中', cancelButtonText: '取消', closeOnClickModal: false}) } catch (_) { return }
      this.acting = true; this.error = ''
      try { const result = await accountsApi.revokeSessions(ids); this.$message.success(`已退出 ${result?.revoked_count || 0} 个会话`); this.selected = [] }
      catch (error) { this.error = error.message || '请求结果不明，已刷新会话列表，请核对' }
      finally { this.acting = false; const reason = this.error; await this.load(true); if (reason) this.error = reason }
    },
    async revokeOthers() {
      if (this.administrator || this.acting) return
      try { await this.$confirm('这会退出当前账号的其他全部有效登录，包括列表下一页及尚未加载的会话。只保留当前登录，应用凭证保持有效。确认退出？', '退出其他全部登录', {type: 'warning', confirmButtonText: '确认退出其他全部', cancelButtonText: '取消', closeOnClickModal: false, distinguishCancelAndClose: true}) } catch (_) { return }
      this.acting = true; this.error = ''
      try { await accountsApi.revokeOtherSessions(); this.$message.success('其他登录已退出'); this.selected = [] }
      catch (error) { this.error = error.message || '请求结果不明，已刷新会话列表，请核对' }
      finally { this.acting = false; const reason = this.error; await this.load(true); if (reason) this.error = reason }
    },
    async logoutCurrent() {
      if (this.acting) return
      try { await this.$confirm('仅退出当前登录，其他会话和应用凭证保持有效。', '退出当前登录', {type: 'warning', confirmButtonText: '确认退出', cancelButtonText: '取消'}) } catch (_) { return }
      this.acting = true
      try { await accountsApi.logout(); this.$store.commit('REMOVE_INFO'); await this.$router.replace('/login') }
      catch (error) { this.error = error.message }
      finally { this.acting = false }
    },
  },
}
</script>
<style scoped>
.login-sessions h2,.login-sessions h3{margin:0}.session-list{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:16px}.session-card{padding:20px;margin:0;min-width:0}.current-session{margin-bottom:24px;border-color:#b9d1ba}.session-top{display:flex;align-items:center;flex-wrap:wrap;gap:10px;margin-bottom:12px}.session-top h3{font-size:16px}.session-client{font-weight:600}.session-actions{margin:24px 0 16px}.session-actions h3{margin-bottom:12px}.session-empty{padding:24px;text-align:center;background:#f8faf7;border-radius:8px}.login-sessions :deep(.el-button){min-height:44px}.login-sessions :deep(.el-checkbox){min-height:44px}
@media(max-width:700px){.session-list{grid-template-columns:1fr}.session-card{padding:16px}.page-toolbar{align-items:flex-start}}
</style>
