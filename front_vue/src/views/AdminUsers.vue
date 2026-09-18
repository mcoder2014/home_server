<template>
  <div class="page-toolbar"><div><h1 class="page-title">用户管理</h1><p class="muted">管理账号、独立功能授权与会话；应用凭证不继承管理员权限。</p></div><el-button type="primary" @click="openCreate">添加用户</el-button></div>
  <section class="surface-card section-card">
    <el-form class="filter-row" @submit.prevent="load(true)">
      <el-input v-model="filters.query" placeholder="用户名 / 显示名称 / 用户 ID" clearable />
      <el-select v-model="filters.status" placeholder="正常与封禁"><el-option label="正常与封禁" value="" /><el-option label="全部（含已删除）" value="all" /><el-option label="正常" value="active" /><el-option label="封禁" value="banned" /><el-option label="已删除" value="deleted" /></el-select>
      <el-select v-model="filters.role" clearable placeholder="全部角色"><el-option label="管理员" value="admin" /><el-option label="普通用户" value="user" /></el-select>
      <el-select v-model="filters.library_enabled" clearable placeholder="家庭藏书权限"><el-option label="藏书已开通" value="true" /><el-option label="藏书未开通" value="false" /></el-select>
      <el-select v-model="filters.webdav_permission" clearable placeholder="WebDAV 权限"><el-option label="未开通" value="none" /><el-option label="只读" value="read" /><el-option label="读写" value="write" /></el-select>
      <el-select v-model="limit"><el-option label="每页 20 条" :value="20" /><el-option label="每页 50 条" :value="50" /></el-select><el-button native-type="submit" :loading="loading">查询</el-button>
    </el-form>
    <el-alert v-if="error" type="error" :title="error" :closable="false" class="form-message" />
    <el-table :data="items" v-loading="loading" row-key="id" empty-text="没有符合条件的用户">
      <el-table-column label="用户" min-width="190"><template #default="{row}"><el-button link type="primary" @click="viewUser(row)"><UserIdentity :user="row" secondary /></el-button></template></el-table-column>
      <el-table-column label="状态 / 角色" min-width="125"><template #default="{row}"><div class="tag-list"><el-tag :type="statusType[row.status]">{{ statusText[row.status] || row.status }}</el-tag><el-tag v-if="row.role === 'admin'" type="warning">管理员</el-tag></div><small v-if="row.must_change_password" class="muted">待首次改密</small></template></el-table-column>
      <el-table-column label="功能权限" min-width="135"><template #default="{row}"><div class="muted">藏书：{{ row.library_enabled ? '已开通' : '未开通' }}</div><div class="muted">WebDAV：{{ webdavText[row.webdav_permission] }}</div></template></el-table-column>
      <el-table-column label="AK/SK · 网页" min-width="135"><template #default="{row}"><div>有效 {{ row.active_application_count || 0 }} / 未吊销 {{ row.application_count || 0 }}</div><div class="muted">发布 {{ row.published_project_count || 0 }} / 未删除 {{ row.project_count || 0 }}</div></template></el-table-column>
      <el-table-column label="有效登录" min-width="150"><template #default="{row}"><el-button link type="primary" @click="viewUser(row, 'sessions')">{{ row.active_session_count == null ? '暂时无法获取' : row.active_session_count + ' 个' }}</el-button><div v-if="row.restricted_session_count" class="muted">{{ row.restricted_session_count }} 个，仅可改密</div><div v-if="row.active_session_count >= warningThreshold" class="session-warning">数量偏多，建议核查</div></template></el-table-column>
      <el-table-column label="最近登录" min-width="175"><template #default="{row}">{{ dateTime(row.last_login_at) }}</template></el-table-column>
      <el-table-column label="操作" width="125" fixed="right"><template #default="{row}"><el-button link type="primary" @click="viewUser(row)">详情</el-button><el-dropdown v-if="row.status !== 'deleted'" trigger="click" @command="action => beginAction(row, action)"><el-button link type="primary">更多⌄</el-button><template #dropdown><el-dropdown-menu>
        <el-dropdown-item v-if="row.status === 'active'" command="ban" :disabled="isSelf(row)">封禁账号</el-dropdown-item><el-dropdown-item v-else command="unban">恢复账号</el-dropdown-item>
        <el-dropdown-item command="logout-all">退出全部会话</el-dropdown-item><el-dropdown-item command="reset-profile">重置昵称 / 头像</el-dropdown-item><el-dropdown-item command="reset-password" :disabled="isSelf(row)">重置密码</el-dropdown-item>
        <el-dropdown-item command="role" :disabled="isSelf(row)" divided>管理员身份</el-dropdown-item><el-dropdown-item command="library-permission">家庭藏书授权</el-dropdown-item><el-dropdown-item command="webdav-permission">WebDAV 授权</el-dropdown-item><el-dropdown-item command="delete" divided :disabled="isSelf(row)">删除账号</el-dropdown-item>
      </el-dropdown-menu></template></el-dropdown></template></el-table-column>
    </el-table><div class="data-footer" v-if="hasMore"><el-button :loading="loading" @click="load(false)">加载更多</el-button></div>
  </section>
  <el-drawer v-model="detailVisible" :title="detail?.user?.user_name || '用户详情'" size="min(820px, 96vw)"><el-skeleton v-if="detailLoading" animated :rows="7" /><template v-else-if="detail">
    <el-tabs v-model="detailTab" class="detail-tabs">
      <el-tab-pane label="基本信息" name="profile"><UserIdentity :user="detail.user" :size="56" secondary /><dl class="meta-list"><dt>用户 ID</dt><dd>{{ detail.user.id }}</dd><dt>用户名</dt><dd>{{ detail.user.user_name }}</dd><dt>昵称</dt><dd>{{ detail.user.display_name || '留空，使用用户名' }}</dd><dt>联系邮箱</dt><dd>{{ detail.user.contact_email || '—' }}</dd><dt>联系电话</dt><dd>{{ detail.user.contact_mobile || '—' }}</dd><dt>状态 / 角色</dt><dd>{{ statusText[detail.user.status] }} / {{ detail.user.role === 'admin' ? '管理员' : '普通用户' }}</dd><dt>家庭藏书</dt><dd>{{ detail.user.library_enabled ? '已开通 · 全站共用库存读写' : '未开通' }}</dd><dt>WebDAV</dt><dd>{{ webdavText[detail.user.webdav_permission] }} · 共享根目录</dd><dt>来源</dt><dd>{{ sourceText[detail.user.source] || detail.user.source || '—' }}</dd><dt>邀请人</dt><dd>{{ detail.user.invited_by_user_id || '—' }}</dd><dt>创建管理员</dt><dd>{{ detail.user.created_by_user_id || '—' }}</dd><dt>{{ detail.user.source === 'config_import' ? '导入时间' : '创建时间' }}</dt><dd>{{ dateTime(detail.user.source === 'config_import' ? detail.user.imported_at || detail.user.create_time : detail.user.create_time) }}</dd><dt>最近登录</dt><dd>{{ dateTime(detail.user.last_login_at) }}</dd></dl><p class="muted">联系信息不自动用于登录或密码找回。原密码及哈希均不可查看。</p><el-button v-if="detail.user.status !== 'deleted'" @click="beginAction(detail.user, 'reset-profile')">重置昵称 / 头像</el-button></el-tab-pane>
      <el-tab-pane label="登录会话" name="sessions"><UserIdentity :user="detail.user" secondary /><LoginSessions v-if="detailTab === 'sessions'" :key="detail.user.id" :user-id="String(detail.user.id)" administrator /><p class="muted">应用凭证和 WebDAV Basic 不计入会话数量。管理员可执行现有全量退出或封禁。</p><div class="action-row" v-if="detail.user.status !== 'deleted'"><el-button type="danger" plain @click="beginAction(detail.user, 'logout-all')">退出此用户全部登录</el-button><el-button v-if="detail.user.status === 'active'" type="danger" :disabled="isSelf(detail.user)" @click="beginAction(detail.user, 'ban')">封禁账号</el-button></div></el-tab-pane>
      <el-tab-pane label="应用凭证"><el-table :data="detail.applications || []" empty-text="暂无应用凭证"><el-table-column prop="name" label="名称" /><el-table-column prop="id" label="ID" /><el-table-column label="状态"><template #default="{row}">{{ applicationStatus[row.status] || row.status }}</template></el-table-column><el-table-column label="权限" min-width="180"><template #default="{row}">{{ (row.scopes || []).join('、') }}</template></el-table-column></el-table><p class="muted">仅显示元信息，不展示或代领他人的 Secret Key。</p></el-tab-pane>
      <el-tab-pane label="网页"><el-table :data="detail.projects || []" empty-text="暂无网页"><el-table-column prop="name" label="名称" /><el-table-column prop="slug" label="路径" /><el-table-column label="发布状态"><template #default="{row}">{{ projectStatus[row.status] || row.status }}</template></el-table-column><el-table-column label="审核状态"><template #default="{row}">{{ moderationText[row.moderation_status] || row.moderation_status }}</template></el-table-column></el-table><el-button style="margin-top:20px" @click="openProjects(detail.user.id)">查看此用户全部网页</el-button></el-tab-pane>
      <el-tab-pane label="邀请"><el-table :data="detail.invitations || []" empty-text="暂无邀请"><el-table-column prop="note" label="备注" /><el-table-column label="状态"><template #default="{row}">{{ invitationStatus[row.status] || row.status }}</template></el-table-column><el-table-column label="到期时间" min-width="175"><template #default="{row}">{{ dateTime(row.expires_at) }}</template></el-table-column><el-table-column label="操作" width="85"><template #default="{row}"><el-button v-if="['unused','active','available'].includes(row.status)" link type="danger" @click="beginRevoke(row)">撤销</el-button></template></el-table-column></el-table></el-tab-pane>
      <el-tab-pane label="操作记录"><p class="muted">按此用户 ID 查看完整管理记录。</p><el-button @click="openAudit(detail.user.id)">查看操作记录</el-button></el-tab-pane>
    </el-tabs>
  </template></el-drawer>
  <el-dialog v-model="createVisible" title="添加用户" width="520px" :close-on-click-modal="false" @closed="clearCreate"><p class="muted">默认普通用户，家庭藏书与 WebDAV 未开通。首次网页登录必须修改初始密码；注册关闭时也可添加。</p><el-alert v-if="createError" type="error" :title="createError" :closable="false" class="form-message" /><el-form label-position="top" @submit.prevent="createUser"><el-form-item label="用户名"><el-input v-model="createForm.user_name" maxlength="32" autocomplete="off" placeholder="3–32 位，以字母或数字开头，可含下划线、短横线" /></el-form-item><el-form-item label="初始密码"><el-radio-group v-model="createForm.generate_password"><el-radio :label="true">安全随机生成</el-radio><el-radio :label="false">手动输入</el-radio></el-radio-group></el-form-item><el-form-item v-if="!createForm.generate_password" label="设置初始密码"><el-input v-model="createForm.password" type="password" autocomplete="new-password" show-password :placeholder="`至少 ${minimum} 个字符`" /></el-form-item><el-button type="primary" native-type="submit" :loading="creating">创建账号</el-button></el-form></el-dialog>
  <AdminConfirm v-model="actionVisible" :title="actionTitles[action] || '撤销邀请码'" :description="actionDescriptions[action] || '撤销后邀请码立即永久失效，不返还生成额度。'" :confirm-name="action === 'delete' ? selected?.user_name : ''" :loading="saving" :server-error="actionError" @confirm="executeAction">
    <p v-if="selected"><UserIdentity :user="selected" secondary /></p>
    <template v-if="action === 'reset-profile'"><el-form-item label="选择要重置的资料（至少一项）"><el-checkbox v-model="actionForm.reset_display_name" :disabled="saving">昵称</el-checkbox><el-checkbox v-model="actionForm.reset_avatar" :disabled="saving">头像</el-checkbox></el-form-item><p class="muted">重置后的展示</p><UserIdentity :user="{...selected, display_name: actionForm.reset_display_name ? '' : selected?.display_name, avatar_url: actionForm.reset_avatar ? '' : selected?.avatar_url}" :size="48" secondary /></template>
    <el-form-item v-if="action === 'role'" label="管理员身份"><el-radio-group v-model="actionForm.role"><el-radio label="user">普通用户</el-radio><el-radio label="admin">管理员</el-radio></el-radio-group></el-form-item>
    <el-form-item v-if="action === 'library-permission'" label="家庭藏书"><el-switch v-model="actionForm.enabled" active-text="开通读写" inactive-text="未开通" /></el-form-item>
    <el-form-item v-if="action === 'webdav-permission'" label="WebDAV 共享目录权限"><el-select v-model="actionForm.permission"><el-option label="未开通" value="none" /><el-option label="只读" value="read" /><el-option label="读写（允许修改、删除文件）" value="write" /></el-select></el-form-item>
    <template v-if="action === 'reset-password'"><el-form-item label="初始密码"><el-radio-group v-model="actionForm.generate_password"><el-radio :label="true">安全随机生成</el-radio><el-radio :label="false">手动输入</el-radio></el-radio-group></el-form-item><el-form-item v-if="!actionForm.generate_password" label="新初始密码"><el-input v-model="actionForm.password" type="password" autocomplete="new-password" show-password /></el-form-item></template>
  </AdminConfirm>
  <el-dialog v-model="secretVisible" title="初始密码已设置" width="540px" :close-on-click-modal="false" @closed="clearSecret"><el-alert title="仅显示一次，请现在保存，通过已有私下渠道交付。" type="warning" :closable="false" /><template v-if="secret"><p>用户名：{{ secret.user?.user_name || selected?.user_name }}</p><div class="secret-value">{{ secret.initial_password }}</div><p class="muted">有效期至 {{ dateTime(secret.password_expires_at) }}（UTC+8），首次网页登录必须修改。</p><el-button type="primary" @click="copySecret">复制初始密码</el-button></template><template #footer><el-button @click="secretVisible = false">已保存，关闭</el-button></template></el-dialog>
</template>
<script>
import AdminConfirm from '@/components/AdminConfirm.vue'
import UserIdentity from '@/components/UserIdentity.vue'
import LoginSessions from '@/components/LoginSessions.vue'
const {accountsApi} = require('@/api/accounts.cjs')
const {dateTime, passwordError, clearOneTimeSecret} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'AdminUsers', components: {AdminConfirm, UserIdentity, LoginSessions},
  data() { return {
    items: [], cursor: '', hasMore: false, loading: false, error: '', limit: 20, warningThreshold: 10, filters: {query: '', status: '', role: '', library_enabled: '', webdav_permission: ''},
    detail: null, detailVisible: false, detailLoading: false, detailTab: 'profile', createVisible: false, creating: false, createError: '', createForm: {user_name: '', generate_password: true, password: ''},
    actionVisible: false, action: '', actionError: '', actionForm: {}, selected: null, invitation: null, saving: false, secret: null, secretVisible: false,
    sourceText: {config_import: '存量导入', import: '存量导入', migration: '存量迁移', config: '配置迁移', invitation: '邀请注册', admin: '管理员创建'}, applicationStatus: {enabled: '已启用', disabled: '已停用', revoked: '已吊销', 1: '已启用', 2: '已停用', 3: '已吊销'}, projectStatus: {draft: '草稿', enabled: '已发布', disabled: '已停用', deleted: '已删除', 1: '草稿', 2: '已发布', 3: '已停用', 4: '已删除'}, moderationText: {normal: '正常', blocked: '管理员已下架', deleted: '管理员已删除'}, invitationStatus: {unused: '未使用', used: '已使用', expired: '已过期', revoked: '已撤销'},
    statusText: {active: '正常', banned: '封禁', deleted: '已删除'}, statusType: {active: 'success', banned: 'warning', deleted: 'info'}, webdavText: {none: '未开通', read: '只读', write: '读写'},
    actionTitles: {'reset-profile': '重置昵称 / 头像', ban: '封禁账号', unban: '恢复账号', delete: '删除账号', 'reset-password': '重置密码', 'logout-all': '退出全部会话', role: '变更管理员身份', 'library-permission': '变更家庭藏书授权', 'webdav-permission': '变更 WebDAV 授权'},
    actionDescriptions: {'reset-profile': '仅重置勾选的昵称或头像；不退出登录、不更改密码及权限。用户仍可重新编辑资料。', ban: '退出所有会话、停用应用、清除家庭藏书与 WebDAV 授权、暂停本人网页访问，并永久撤销未用邀请码。', unban: '恢复后需重新登录；应用保持停用，家庭藏书和 WebDAV 需重新授权，旧邀请与审核锁不会恢复。', delete: '账号不可恢复。应用永久吊销，网页停止访问并进入删除清理。用户名继续保留，不可重新注册。', 'reset-password': '设置临时密码并要求首次改密；退出所有会话、停用应用，清除家庭藏书与 WebDAV 授权。', 'logout-all': '退出此用户所有登录会话，并停用应用凭证。已发送到设备的内容无法撤回。', role: '管理员可以管理其他账号、查看所有私有网页并删除内容。角色变更使旧会话失效；不能撤销自己的身份或最后一个可用管理员。', 'library-permission': '开通后可查看和修改全站共用藏书。撤销会立即阻止后续读写，并移除相关应用 scope。', 'webdav-permission': 'WebDAV 访问全站共享根目录，可能读取敏感文件。撤销或降权对后续请求生效，并更新相关应用权限。'},
  } },
  computed: {minimum() { return this.$store.state.userInfo?.password_policy?.min_length || 15 }},
  created() { this.load(true) },
  beforeUnmount() { this.clearSecret(); this.clearCreate(); this.actionForm = {} },
  watch: {actionVisible(value) { if (!value) this.actionForm = {} }},
  methods: {
    dateTime,
    isSelf(row) { return String(row.id) === String(this.$store.state.userInfo?.id) },
    async load(reset) { this.loading = true; this.error = ''; try { const data = await accountsApi.users({...this.filters, limit: this.limit, cursor: reset ? '' : this.cursor}); this.items = reset ? data.items : this.items.concat(data.items); this.cursor = data.next_cursor || ''; this.hasMore = !!data.has_more; this.warningThreshold = data.session_warning_threshold || 10 } catch (error) { this.error = error.message } finally { this.loading = false } },
    async viewUser(row, tab = 'profile') { this.detailTab = tab; this.detailVisible = true; this.detailLoading = true; this.detail = null; try { this.detail = await accountsApi.user(row.id) } catch (error) { this.error = error.message; this.detailVisible = false } finally { this.detailLoading = false } },
    openProjects(id) { this.detailVisible = false; this.$router.push({path: '/admin/web-share', query: {owner_user_id: id}}) },
    openAudit(id) { this.detailVisible = false; this.$router.push({path: '/admin/audit-logs', query: {target_type: 'user', target_id: id}}) },
    openCreate() { this.clearCreate(); this.createVisible = true },
    clearCreate() { this.createForm = {user_name: '', generate_password: true, password: ''}; this.createError = '' },
    async createUser() {
      this.createError = !/^[a-zA-Z0-9][a-zA-Z0-9_-]{2,31}$/.test(this.createForm.user_name) ? '用户名需要 3–32 位，以字母或数字开头，可含下划线、短横线' : !this.createForm.generate_password ? passwordError(this.createForm.password, this.createForm.password, this.minimum) : ''
      if (this.createError || this.creating) return
      this.creating = true
      const data = {user_name: this.createForm.user_name, generate_password: this.createForm.generate_password}
      if (!data.generate_password) data.password = this.createForm.password
      try { this.secret = await accountsApi.createUser(data); this.secretVisible = true; this.createVisible = false; this.clearCreate(); await this.load(true) }
      catch (error) { this.createError = error.message }
      finally { this.creating = false }
    },
    beginAction(user, action) { this.selected = user; this.action = action; this.actionError = ''; this.actionForm = {role: user.role, enabled: !!user.library_enabled, permission: user.webdav_permission || 'none', generate_password: true, password: '', reset_display_name: false, reset_avatar: false}; this.actionVisible = true },
    beginRevoke(invitation) { this.invitation = invitation; this.beginAction(this.detail.user, 'revoke-invitation') },
    // 合并管理员确认信息与选定动作参数，带账号版本提交；处理一次性密码、本人会话退出及并发修改冲突。
    async executeAction(confirmation) {
      if (this.saving) return
      const data = {...confirmation}
      if (this.action === 'reset-profile') {
        data.reset_display_name = !!this.actionForm.reset_display_name; data.reset_avatar = !!this.actionForm.reset_avatar
        if (!data.reset_display_name && !data.reset_avatar) { this.actionError = '请至少选择昵称或头像一项'; return }
      }
      if (this.action === 'role') data.role = this.actionForm.role
      if (this.action === 'library-permission') data.enabled = this.actionForm.enabled
      if (this.action === 'webdav-permission') data.permission = this.actionForm.permission
      if (this.action === 'reset-password') {
        data.generate_password = this.actionForm.generate_password
        if (!data.generate_password) { this.actionError = passwordError(this.actionForm.password, this.actionForm.password, this.minimum); if (this.actionError) return; data.password = this.actionForm.password }
      }
      this.saving = true; this.actionError = ''
      try {
        const result = this.action === 'revoke-invitation' ? await accountsApi.revokeUserInvitation(this.selected.id, this.invitation.id, this.selected.revision, data) : await accountsApi.userAction(this.selected.id, this.action, this.selected.revision, data)
        this.actionVisible = false
        if (result?.initial_password) { this.secret = result; this.secretVisible = true }
        else this.$message.success('操作已完成')
        if (this.isSelf(this.selected) && ['logout-all', 'reset-password'].includes(this.action)) { this.$store.commit('REMOVE_INFO'); await this.$router.replace('/login'); return }
        if (this.isSelf(this.selected) && this.action === 'reset-profile') { await this.$store.dispatch('refreshSession'); this.$store.dispatch('publishProfileUpdate') }
        if (this.detailVisible) await this.viewUser(this.selected, this.detailTab)
        await this.load(true)
      } catch (error) {
        this.actionError = error.status === 409 ? '账号已被其他操作修改，请关闭弹窗，刷新列表后重新确认。' : error.message
      } finally { this.saving = false }
    },
    clearSecret() { this.secret = clearOneTimeSecret(this.secret) },
    async copySecret() { try { await navigator.clipboard.writeText(this.secret.initial_password); this.$message.success('已复制') } catch (_) { this.$message.warning('复制失败，请手动选中密码复制') } },
  },
}
</script>

<style scoped>.session-warning{color:#a66a1f;font-size:12px;margin-top:6px}</style>
