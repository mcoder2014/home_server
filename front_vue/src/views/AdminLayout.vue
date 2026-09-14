<template>
  <MyHeader />
  <main class="page-container admin-layout">
    <aside class="admin-sidebar"><span class="page-eyebrow">管理中心</span><nav aria-label="管理导航"><router-link to="/admin/users">用户管理<span>账号与功能授权</span></router-link><router-link to="/admin/web-share">网页审核<span>全站内容与风险处置</span></router-link><router-link to="/admin/config">站点设置<span>动态配置与版本发布</span></router-link><router-link to="/admin/audit-logs">操作记录<span>管理行为追溯</span></router-link></nav><p class="muted">管理员访问来自当前账号。应用凭证不能执行管理操作。</p></aside>
    <div class="admin-content"><router-view /></div>
  </main>
</template>
<script>
import MyHeader from '@/components/MyHeader'
export default {name: 'AdminLayout', components: {MyHeader}}
</script>
<style scoped>
.admin-layout{display:grid;grid-template-columns:190px minmax(0,1fr);gap:30px;max-width:1440px}.admin-sidebar{padding-top:8px}.admin-sidebar nav{display:flex;flex-direction:column;gap:8px;margin:20px 0}.admin-sidebar a{text-decoration:none;color:var(--text-primary);padding:14px;border-radius:10px;font-size:14px}.admin-sidebar a span{display:block;color:var(--text-secondary);font-size:11px;margin-top:7px}.admin-sidebar a.router-link-active{background:#e6eee7;color:var(--primary-dark);font-weight:600}.admin-content{min-width:0}.admin-sidebar .muted{font-size:11px;padding:0 14px}@media(max-width:900px){.admin-layout{grid-template-columns:1fr;gap:10px}.admin-sidebar nav{flex-direction:row;margin:8px 0 12px}.admin-sidebar a{flex:1;text-align:center;padding:12px 6px}.admin-sidebar a span,.admin-sidebar p,.admin-sidebar>.page-eyebrow{display:none}}
</style>
