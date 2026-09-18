<template>
  <span class="user-identity" :class="{'identity-detail': secondary}" :style="{'--avatar-size': size + 'px'}">
    <span class="identity-avatar" aria-hidden="true">
      <img v-if="identity.avatar && !failed && !hideAvatar" :src="identity.avatar" alt="" @error="failed = true" />
      <span v-else-if="identity.initial">{{ identity.initial }}</span>
      <svg v-else viewBox="0 0 24 24" fill="currentColor"><path d="M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8Zm-7 8v-2a7 7 0 0 1 14 0v2H5Z" /></svg>
    </span>
    <span v-if="!avatarOnly" class="identity-text"><span class="identity-name">{{ identity.name }}</span><small v-if="secondary" class="identity-secondary">{{ user?.user_name ? '@' + user.user_name : '' }}{{ identifier ? ' / ID ' + identifier : '' }}</small></span>
  </span>
</template>
<script>
const {userIdentity} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'UserIdentity',
  props: {user: {type: Object, default: () => ({})}, size: {type: Number, default: 32}, secondary: Boolean, avatarOnly: Boolean, hideAvatar: Boolean},
  data() { return {failed: false} },
  computed: {identity() { return userIdentity(this.user || {}) }, identifier() { return this.user?.user_id || this.user?.id || '' }},
  watch: {'identity.avatar'() { this.failed = false }},
}
</script>
<style scoped>
.user-identity{display:inline-flex;align-items:center;gap:9px;min-width:0;max-width:100%;vertical-align:middle;color:var(--text-primary)}
.identity-avatar{display:grid;place-items:center;flex-shrink:0;width:var(--avatar-size);height:var(--avatar-size);overflow:hidden;border-radius:50%;background:#edf0e7;color:#687447;font-size:calc(var(--avatar-size) * .4);font-weight:650}
.identity-avatar img{width:100%;height:100%;object-fit:cover}.identity-avatar svg{width:65%;height:65%}.identity-text{display:flex;flex-direction:column;min-width:0;line-height:1.45}.identity-name{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.identity-secondary{color:var(--text-secondary);font-size:11px;overflow-wrap:anywhere}
</style>
