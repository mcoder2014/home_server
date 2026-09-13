<template>
  <div class="page-toolbar"><div><h1 class="page-title">操作记录</h1><p class="muted">记录账号、内容与配置的管理动作，时间为 UTC+8。</p></div><el-button :loading="loading" @click="load(true)">刷新</el-button></div>
  <section class="surface-card section-card">
    <el-form class="filter-row" @submit.prevent="load(true)"><el-select v-model="filters.target_type" clearable placeholder="全部对象"><el-option label="用户" value="user" /><el-option label="网页" value="web_project" /><el-option label="配置" value="site_config" /><el-option label="邀请码" value="invitation" /></el-select><el-input v-model="filters.target_id" placeholder="对象 ID（数字）" clearable /><el-button native-type="submit" :loading="loading">查询</el-button></el-form>
    <el-alert v-if="error" type="error" :title="error" :closable="false" class="form-message" />
    <el-table :data="items" v-loading="loading" empty-text="暂无符合条件的记录"><el-table-column type="expand"><template #default="{row}"><dl class="meta-list audit-detail"><dt>记录 ID</dt><dd>{{ row.id }}</dd><dt>变更前</dt><dd><pre>{{ formatSummary(row.before || row.before_summary) }}</pre></dd><dt>变更后</dt><dd><pre>{{ formatSummary(row.after || row.after_summary) }}</pre></dd><dt>请求 ID</dt><dd>{{ row.request_id || '—' }}</dd></dl></template></el-table-column><el-table-column label="时间" min-width="178"><template #default="{row}">{{ dateTime(row.create_time) }}</template></el-table-column><el-table-column label="操作者" min-width="140"><template #default="{row}">{{ row.actor_user_name || row.actor_user_id || '系统' }}</template></el-table-column><el-table-column label="动作" min-width="150"><template #default="{row}">{{ actionText[row.action] || row.action }}</template></el-table-column><el-table-column label="对象" min-width="150"><template #default="{row}">{{ targetText[row.target_type] || row.target_type }} / {{ row.target_id }}</template></el-table-column><el-table-column prop="reason" label="原因" min-width="190" show-overflow-tooltip /></el-table>
    <div class="data-footer" v-if="hasMore"><el-button :loading="loading" @click="load(false)">加载更多</el-button></div>
  </section>
</template>
<script>
const {accountsApi} = require('@/api/accounts.cjs')
const {dateTime} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'AdminAudit',
  data() { return {actionText: {ban: '封禁账号', unban: '恢复账号', delete: '删除账号', 'reset-password': '重置密码', 'logout-all': '退出全部会话', role: '变更管理员身份', 'library-permission': '变更家庭藏书授权', 'webdav-permission': '变更 WebDAV 授权', profile: '更新个人资料', 'change-password': '修改密码', register: '邀请注册', create: '创建账号', 'revoke-invitation': '撤销邀请码', 'config.publish': '发布配置', 'config.rollback': '回滚配置'}, targetText: {user: '用户', web_project: '网页', site_config: '站点配置', invitation: '邀请码'}, items: [], cursor: '', hasMore: false, loading: false, error: '', filters: {target_type: this.$route.query.target_type || '', target_id: this.$route.query.target_id || ''}} },
  created() { this.load(true) },
  methods: {
    dateTime,
    formatSummary(value) { if (!value) return '—'; if (typeof value === 'string') { try { return JSON.stringify(JSON.parse(value), null, 2) } catch (_) { return value } } return JSON.stringify(value, null, 2) },
    async load(reset) { this.loading = true; this.error = ''; try { const result = await accountsApi.auditLogs({...this.filters, cursor: reset ? '' : this.cursor, limit: 20}); this.items = reset ? result.items : this.items.concat(result.items); this.cursor = result.next_cursor || ''; this.hasMore = !!result.has_more } catch (error) { this.error = error.message } finally { this.loading = false } },
  },
}
</script>
<style scoped>.audit-detail{padding:20px 30px}.audit-detail pre{margin:0;white-space:pre-wrap;font-size:12px;overflow-wrap:anywhere}</style>
