<template>
  <div class="session-details"><dl><dt>登录时 IP</dt><dd>{{ session.login_ip || '未知' }}</dd><dt>{{ session.authenticated_at && !String(session.authenticated_at).startsWith('0001-') ? '登录时间' : '会话创建时间' }}</dt><dd>{{ dateTime(session.authenticated_at && !String(session.authenticated_at).startsWith('0001-') ? session.authenticated_at : session.create_time) }}</dd><dt>到期时间</dt><dd>{{ dateTime(session.expire_time) }}<small>{{ remainingText(remaining) }}</small></dd><dt>来源 / 用途</dt><dd>{{ sources[session.login_source] || '历史来源未知' }} / {{ session.purpose === 'password_change' ? '仅可改密' : '普通登录' }}</dd></dl><details><summary>查看客户端原文与会话 ID</summary><p class="muted">会话 ID：{{ session.id }}</p><pre>{{ session.user_agent || 'UA 未记录' }}</pre></details></div>
</template>
<script>
const {dateTime, remainingText} = require('@/utils/accounts_behavior.cjs')
export default {
  name: 'SessionDetails',
  props: {session: {type: Object, required: true}, remaining: {type: Number, default: null}},
  data() { return {sources: {web: '网站登录', legacy: '旧版客户端', legacy_client: '旧版客户端', passport: '旧版客户端'}} },
  methods: {dateTime, remainingText},
}
</script>
<style scoped>
dl{display:grid;grid-template-columns:100px minmax(0,1fr);gap:10px;margin:16px 0;font-size:13px}dt{color:var(--text-secondary)}dd{margin:0;overflow-wrap:anywhere}small{display:block;color:var(--text-secondary);margin-top:4px}details{margin:12px 0}summary{cursor:pointer;min-height:36px;color:var(--text-secondary)}pre{margin:4px 0;white-space:pre-wrap;overflow-wrap:anywhere;font:12px/1.6 monospace;max-height:150px;overflow:auto}
@media(max-width:420px){dl{grid-template-columns:85px minmax(0,1fr)}}
</style>
