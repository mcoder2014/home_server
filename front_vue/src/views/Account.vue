<template>
  <MyHeader />
  <main class="page-container">
    <div class="page-eyebrow">账户设置</div><h1 class="page-title">个人中心</h1>
    <el-alert v-if="user?.must_change_password" type="warning" title="请先修改管理员提供的初始密码" description="修改完成后需重新登录，随后才能使用网站页面功能；已授权的 WebDAV 客户端可直接使用未过期密码。" :closable="false" show-icon class="form-message" />
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="form-message" />
    <el-tabs v-model="tab" @tab-change="changeTab">
      <el-tab-pane label="个人资料" name="profile" :disabled="user?.must_change_password">
        <div class="account-grid">
          <section class="surface-card section-card">
            <h2>个人资料</h2>
            <div class="profile-avatar"><UserIdentity :user="user || {}" :size="72" secondary /><div class="action-row"><el-button :disabled="saving || conflict" @click="avatarVisible = true">更换头像</el-button><el-button :disabled="saving || conflict || !user?.avatar_url" @click="removeAvatar">移除头像</el-button></div></div>
            <el-alert v-if="conflict" type="warning" title="资料已发生变化，草稿已保留" description="请先查看最新资料，再决定使用草稿覆盖相应资料字段。" :closable="false" class="form-message" />
            <el-form label-position="top" @submit.prevent="saveProfile">
              <el-form-item label="昵称" :error="profileErrors.display_name"><el-input v-model="profile.display_name" maxlength="128" :placeholder="`留空使用 ${user?.user_name || '登录用户名'}`" /><small class="nickname-count" :class="{'danger-note': nicknameLength > 64}">{{ nicknameLength }} / 64 个 Unicode 字符</small></el-form-item>
              <p class="muted">展示名称：{{ profile.display_name.trim() || user?.user_name }}。昵称不用于登录，最多 64 个 Unicode 字符。</p>
              <el-form-item label="联系邮箱" :error="profileErrors.contact_email"><el-input v-model="profile.contact_email" type="email" autocomplete="email" maxlength="254" /></el-form-item>
              <el-form-item label="联系电话" :error="profileErrors.contact_mobile"><el-input v-model="profile.contact_mobile" type="tel" autocomplete="tel" maxlength="32" /></el-form-item>
              <p class="muted">联系信息仅用于沟通，不用于登录或找回密码。修改资料不会更换已有登录别名。</p>
              <div class="action-row"><el-button type="primary" native-type="submit" :loading="saving" :disabled="!dirty || conflict">保存资料</el-button><el-button :disabled="saving || !dirty" @click="resetProfile">取消修改</el-button><el-button v-if="conflict" @click="loadLatest">查看最新资料</el-button></div>
            </el-form>
            <el-dialog v-model="latestVisible" title="最新个人资料" width="min(540px, 96vw)"><UserIdentity :user="latest || {}" :size="48" secondary /><dl class="meta-list"><dt>昵称</dt><dd>{{ latest?.display_name || '留空，使用用户名' }}</dd><dt>联系邮箱</dt><dd>{{ latest?.contact_email || '—' }}</dd><dt>联系电话</dt><dd>{{ latest?.contact_mobile || '—' }}</dd></dl><template #footer><el-button @click="discardDraft">使用最新资料</el-button><el-button type="primary" @click="keepDraft">保留草稿继续编辑</el-button></template></el-dialog>
          </section>
          <section class="surface-card section-card"><h2>账号与功能权限</h2><dl class="meta-list"><dt>用户 ID</dt><dd>{{ user?.id }}</dd><dt>登录用户名</dt><dd>{{ user?.user_name }}</dd><dt>{{ user?.source === 'config_import' ? '导入时间' : '创建时间' }}</dt><dd>{{ dateTime(user?.source === 'config_import' ? user?.imported_at || user?.create_time : user?.create_time) }}</dd><dt>账号角色</dt><dd>{{ user?.role === 'admin' ? '管理员' : '普通用户' }}</dd><dt>家庭藏书</dt><dd>{{ user?.library_enabled ? '已开通 · 可查看和修改' : '未开通，请联系管理员' }}</dd><dt>WebDAV</dt><dd>{{ webdavText }}</dd></dl><p class="muted">家庭藏书为全站共用库存。管理员身份不自动开通家庭藏书或 WebDAV。</p></section>
        </div>
      </el-tab-pane>
      <el-tab-pane label="账户安全" name="security">
        <section class="surface-card section-card account-narrow"><h2>{{ user?.must_change_password ? '修改初始密码' : '修改密码' }}</h2>
          <p class="danger-note">修改后包括当前设备在内的全部网站登录失效，应用凭据停用，WebDAV 客户端需要更新密码。你需要重新登录；重新启用应用前请核对来源。</p>
          <el-form label-position="top" @submit.prevent="changePassword">
            <el-form-item label="当前密码"><el-input v-model="password.current_password" type="password" show-password autocomplete="current-password" /></el-form-item>
            <el-form-item label="新密码" :error="passwordValidation"><el-input v-model="password.new_password" type="password" show-password autocomplete="new-password" :placeholder="`至少 ${minimum} 个字符，允许空格和粘贴`" /></el-form-item>
            <el-form-item label="确认新密码"><el-input v-model="password.confirm_password" type="password" show-password autocomplete="new-password" /></el-form-item>
            <el-button type="primary" native-type="submit" :loading="saving">确认修改并退出登录</el-button>
          </el-form>
        </section>
        <section v-if="!user?.must_change_password" class="surface-card section-card account-narrow"><h2>登录与应用凭证</h2><p class="muted">登录管理可仅退出指定会话或其他登录。应用凭证与 WebDAV Basic 不计入网站会话数量。忘记密码请联系管理员重置。</p><div class="action-row"><el-button @click="changeTab('sessions')">查看登录管理</el-button><el-button @click="$router.push('/applications')">管理应用凭证</el-button></div></section>
        <section v-if="!user?.must_change_password" class="surface-card section-card account-narrow"><h2>退出全部登录</h2><p class="danger-note">包括当前登录在内的全部网站会话失效，应用凭据停用。WebDAV Basic 的原密码仍可使用。</p><el-button type="danger" plain :loading="saving" @click="logoutAll">退出全部登录会话</el-button></section>
      </el-tab-pane>
      <el-tab-pane label="登录管理" name="sessions" :disabled="user?.must_change_password"><LoginSessions v-if="tab === 'sessions' && !user?.must_change_password" /></el-tab-pane>
    </el-tabs>
    <AvatarCropper v-if="!user?.must_change_password" v-model="avatarVisible" :revision="revision" :conflict="conflict" @saved="acceptAvatar" @conflict="avatarConflict" @draft-change="avatarDraft = $event" />
  </main>
</template>
<script>
import MyHeader from '@/components/MyHeader'
import UserIdentity from '@/components/UserIdentity.vue'
import AvatarCropper from '@/components/AvatarCropper.vue'
import LoginSessions from '@/components/LoginSessions.vue'
const {accountsApi} = require('@/api/accounts.cjs')
const {profilePayload, profileError, preserveProfileDraft, passwordError, dateTime} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'AccountPage', components: {MyHeader, UserIdentity, AvatarCropper, LoginSessions},
  data() { return {tab: this.$route.path.endsWith('/security') ? 'security' : this.$route.path.endsWith('/sessions') ? 'sessions' : 'profile', profile: {display_name: '', contact_email: '', contact_mobile: ''}, savedProfile: {}, revision: '', saving: false, error: '', profileErrors: {}, passwordValidation: '', conflict: false, latest: null, latestVisible: false, avatarVisible: false, avatarDraft: false, password: {current_password: '', new_password: '', confirm_password: ''}} },
  computed: {
    minimum() { return this.user?.password_policy?.min_length || 15 },
    nicknameLength() { return Array.from(this.profile.display_name.trim()).length },
    user() { return this.$store.state.userInfo },
    dirty() { return Object.keys(profilePayload(this.profile, this.savedProfile)).length > 0 },
    webdavText() { return {none: '未开通，请联系管理员', read: '共享目录 · 只读', write: '共享目录 · 读写'}[this.user?.webdav_permission] || '未开通' },
  },
  created() { this.resetProfile() },
  mounted() { window.addEventListener('beforeunload', this.beforeUnload) },
  beforeUnmount() { window.removeEventListener('beforeunload', this.beforeUnload); this.password = {current_password: '', new_password: '', confirm_password: ''} },
  async beforeRouteLeave() {
    if ((!this.dirty && !this.avatarDraft) || this.user?.must_change_password || !this.user) return true
    try { await this.$confirm('个人资料尚未保存，离开会丢失修改。', '离开个人中心', {type: 'warning', confirmButtonText: '离开', cancelButtonText: '继续编辑'}); return true } catch (_) { return false }
  },
  watch: {'$route.path'(path) { this.tab = path.endsWith('/security') ? 'security' : path.endsWith('/sessions') ? 'sessions' : 'profile' }, user(value) { if (value) this.applyIdentity(value) }},
  methods: {
    dateTime,
    beforeUnload(event) { if (this.dirty || this.avatarDraft) { event.preventDefault(); event.returnValue = '' } },
    changeTab(tab) { this.$router.replace(tab === 'security' ? '/account/security' : tab === 'sessions' ? '/account/sessions' : '/account') },
    resetProfile() { this.profile = profilePayload(this.user || {}); this.savedProfile = {...this.profile}; this.revision = this.user?.revision; this.conflict = false; this.error = ''; this.profileErrors = {} },
    async saveProfile() {
      if (this.saving || this.conflict) return
      this.error = profileError(this.profile)
      this.profileErrors = {}
      if (this.error) {
        this.profileErrors[this.error.includes('邮箱') ? 'contact_email' : this.error.includes('电话') ? 'contact_mobile' : 'display_name'] = this.error
        return
      }
      this.saving = true
      try { const me = await accountsApi.profile(this.revision, profilePayload(this.profile, this.savedProfile)); this.$store.commit('SET_USERINFO', me); this.resetProfile(); this.$store.dispatch('publishProfileUpdate'); this.$message.success('个人资料已保存') }
      catch (error) { this.error = error.message; if (error.status === 409) this.conflict = true }
      finally { this.saving = false }
    },
    async loadLatest() { try { this.latest = await accountsApi.me(); this.latestVisible = true } catch (error) { this.error = error.message } },
    discardDraft() { this.$store.commit('SET_USERINFO', this.latest); this.resetProfile(); this.latestVisible = false },
    keepDraft() { this.profile = preserveProfileDraft(this.profile, this.savedProfile, this.latest); this.$store.commit('SET_USERINFO', this.latest); this.savedProfile = profilePayload(this.latest); this.revision = this.latest.revision; this.conflict = false; this.latestVisible = false; this.error = '' },
    applyIdentity(value) {
      if (String(value.revision) === String(this.revision)) return
      if (!this.dirty && !this.avatarDraft) { this.resetProfile(); return }
      this.latest = value; this.conflict = true
    },
    acceptAvatar(value) {
      this.profile = preserveProfileDraft(this.profile, this.savedProfile, value)
      this.savedProfile = profilePayload(value); this.revision = value.revision
      this.$store.commit('SET_USERINFO', value); this.$store.dispatch('publishProfileUpdate')
      this.error = ''; this.$message.success(value.avatar_url ? '头像已保存' : '已恢复默认头像')
    },
    async avatarConflict() { this.conflict = true; await this.loadLatest() },
    async removeAvatar() {
      if (this.saving || this.conflict) return
      try { await this.$confirm('移除后恢复默认头像，未保存的昵称草稿会保留。', '移除头像', {confirmButtonText: '确认移除', cancelButtonText: '取消', type: 'warning'}) } catch (_) { return }
      this.saving = true
      try { this.acceptAvatar(await accountsApi.removeAvatar(this.revision)) }
      catch (error) { this.error = error.message; if (error.status === 409) this.conflict = true }
      finally { this.saving = false }
    },
    async changePassword() {
      this.error = ''; this.passwordValidation = passwordError(this.password.new_password, this.password.confirm_password, this.minimum)
      if (!this.password.current_password) { this.error = '请输入当前密码'; return }
      if (this.passwordValidation || this.saving) return
      this.saving = true
      try { await accountsApi.changePassword({...this.password}); this.password = {current_password: '', new_password: '', confirm_password: ''}; this.savedProfile = {...this.profile}; this.$store.commit('REMOVE_INFO'); await this.$router.replace({path: '/login', query: {changed: '1'}}) }
      catch (error) { this.error = error.message }
      finally { this.saving = false }
    },
    async logoutAll() {
      try { await this.$confirm('这会退出包括当前设备在内的全部网站登录，并停用应用凭据。WebDAV Basic 的原密码仍可使用。', '退出全部会话', {type: 'warning', confirmButtonText: '确认退出全部', cancelButtonText: '取消', closeOnClickModal: false}) } catch (_) { return }
      this.saving = true
      try { await accountsApi.logoutAll(); this.savedProfile = {...this.profile}; this.$store.commit('REMOVE_INFO'); await this.$router.replace('/login') }
      catch (error) { this.error = error.message }
      finally { this.saving = false }
    },
  },
}
</script>
<style scoped>
.profile-avatar{display:flex;flex-wrap:wrap;align-items:center;gap:18px;margin:20px 0 28px}.profile-avatar .action-row{margin:0}.page-container :deep(.el-button){min-height:44px}.nickname-count{display:block;margin-top:4px;color:var(--text-secondary)}.nickname-count.danger-note{color:var(--danger-color,#b74332)}
</style>
