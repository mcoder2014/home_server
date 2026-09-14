<template>
  <MyHeader />
  <main class="page-container">
    <div class="page-toolbar"><div><div class="page-eyebrow">把空间分享给认识的人</div><h1 class="page-title">邀请朋友</h1><p class="muted">每月最多 3 个，一次使用，固定有效 7 天。撤销、过期或使用均不返还额度。</p></div><el-button :loading="loading" @click="load">刷新</el-button></div>
    <el-alert v-if="error" type="error" :title="error" :closable="false" class="form-message" />
    <section class="surface-card section-card"><div class="invitation-summary"><div><strong>本月剩余 {{ quota.remaining }} / {{ quota.monthly_limit || 3 }} 次</strong><p class="muted">下次额度更新：{{ dateTime(quota.next_reset_at) }}（UTC+8）</p></div><el-tag :type="quota.enabled ? 'success' : 'info'">{{ quota.enabled ? '注册已开放' : '注册已关闭' }}</el-tag></div>
      <el-alert v-if="!quota.enabled" title="网站已关闭注册，无法生成或使用邀请码。管理员重新开启后，旧码不会恢复。" type="info" :closable="false" class="form-message" />
      <p v-else-if="quota.remaining === 0" class="muted">本月额度已用完，或新账号尚未到次月邀请资格时间。</p>
      <el-form class="inline-field" @submit.prevent="generate"><el-input v-model="note" maxlength="100" placeholder="备注（可选），例如朋友称呼" :disabled="!quota.enabled || generating" /><el-button type="primary" native-type="submit" :disabled="!quota.enabled || quota.remaining <= 0 || loading" :loading="generating">生成邀请码</el-button></el-form>
    </section>
    <section class="surface-card section-card"><h2>邀请记录</h2><el-table :data="quota.items || []" v-loading="loading" empty-text="还没有生成过邀请码"><el-table-column prop="note" label="备注" min-width="140" /><el-table-column label="状态" width="130"><template #default="{row}"><el-tag>{{ statusText[row.status] || row.status }}</el-tag></template></el-table-column><el-table-column label="生成时间" min-width="175"><template #default="{row}">{{ dateTime(row.create_time) }}</template></el-table-column><el-table-column label="到期时间（UTC+8）" min-width="175"><template #default="{row}">{{ dateTime(row.expires_at) }}</template></el-table-column><el-table-column prop="used_by_user_name" label="已注册用户" min-width="140" /><el-table-column label="操作" width="90"><template #default="{row}"><el-button v-if="['unused','active','available'].includes(row.status)" type="danger" link :loading="revoking === row.id" @click="revoke(row)">撤销</el-button></template></el-table-column></el-table></section>
    <el-dialog v-model="secretVisible" title="邀请码已生成" width="560px" :close-on-click-modal="false" @closed="clearSecret"><el-alert title="完整邀请码仅展示这一次，请现在复制并私下交给朋友。" type="warning" :closable="false" /><template v-if="secret"><p class="muted">到期：{{ dateTime(secret.invitation?.expires_at) }}（UTC+8）</p><div class="secret-value">{{ secret.code }}</div><div class="action-row" style="margin-top:16px"><el-button :disabled="!quota.enabled" @click="copy(secret.code)">复制邀请码</el-button><el-button type="primary" :disabled="!quota.enabled" @click="copy(inviteURL)">复制邀请链接</el-button></div></template><template #footer><el-button @click="secretVisible = false">已保存，关闭</el-button></template></el-dialog>
  </main>
</template>
<script>
import MyHeader from '@/components/MyHeader'
const {accountsApi} = require('@/api/accounts.cjs')
const {dateTime, requestID, clearOneTimeSecret} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'InvitationsPage', components: {MyHeader},
  data() { return {loading: false, generating: false, revoking: '', note: '', pendingRequest: null, quota: {items: [], remaining: 0, enabled: false}, error: '', secret: null, secretVisible: false, statusText: {unused: '未使用', active: '未使用', available: '未使用', used: '已使用', expired: '已过期', revoked: '已撤销', invalidated: '已失效', disabled: '已失效'}} },
  computed: {inviteURL() { return this.secret ? `${window.location.origin}/register#invite=${encodeURIComponent(this.secret.code)}` : '' }},
  created() { this.load() },
  beforeUnmount() { this.clearSecret() },
  methods: {
    dateTime,
    async load() { this.loading = true; try { this.quota = await accountsApi.invitations(); if (!this.quota.enabled) { this.clearSecret(); this.secretVisible = false } } catch (error) { this.error = error.message } finally { this.loading = false } },
    async generate() {
      if (this.generating || !this.quota.enabled || this.quota.remaining <= 0) return
      if (new TextEncoder().encode(this.note).length > 128) { this.error = '备注最多 128 个 UTF-8 字节（中文通常占 3 字节）'; return }
      this.generating = true; this.error = ''
      if (!this.pendingRequest) this.pendingRequest = {request_id: requestID(), note: this.note}
      try {
        const result = await accountsApi.createInvitation(this.pendingRequest)
        this.pendingRequest = null; this.note = ''
        if (result.code) { this.secret = result; this.secretVisible = true }
        else this.$message.warning('上次生成已成功；完整邀请码无法再次显示，可在记录中撤销。')
        await this.load()
      } catch (error) { this.error = error.message; if (error.status && error.status < 500) this.pendingRequest = null }
      finally { this.generating = false }
    },
    async revoke(row) {
      try { await this.$confirm('撤销后无法使用，也不会返还本月额度。', '撤销邀请码', {confirmButtonText: '确认撤销', cancelButtonText: '取消', type: 'warning'}) } catch (_) { return }
      this.revoking = row.id
      try { await accountsApi.revokeInvitation(row.id); this.$message.success('邀请码已撤销'); await this.load() } catch (error) { this.error = error.message } finally { this.revoking = '' }
    },
    clearSecret() { this.secret = clearOneTimeSecret(this.secret) },
    async copy(text) { try { await navigator.clipboard.writeText(text); this.$message.success('已复制') } catch (_) { this.$message.warning('复制失败，请选中邀请码手动复制') } },
  },
}
</script>
<style scoped>.invitation-summary {display:flex;justify-content:space-between;gap:20px;align-items:flex-start}.invitation-summary strong {font-size:22px}</style>
