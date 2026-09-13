<template>
  <div>
    <MyHeader />
    <main class="page-container">
      <div class="action-bar project-list-actions">
        <div>
          <span class="page-eyebrow">WEB HOSTING</span>
          <h1 class="page-title">网页托管</h1>
          <p class="page-desc">为 HTML 文档和静态网页提供固定地址，并控制谁可以访问。</p>
        </div>
        <el-button type="primary" size="large" @click="$router.push('/web-share/new')">
          <el-icon><Plus /></el-icon>新建托管
        </el-button>
      </div>

      <section class="card projects-card" aria-label="托管内容列表">
        <div class="list-filter">
          <el-radio-group v-model="statusFilter" @change="loadProjects(true)">
            <el-radio-button label="active">当前托管</el-radio-button>
            <el-radio-button label="deleted">回收站</el-radio-button>
          </el-radio-group>
          <el-button :loading="loading" @click="loadProjects(true)"><el-icon><Refresh /></el-icon>刷新</el-button>
        </div>

        <el-table v-loading="loading" :data="projects" class="desktop-projects">
          <el-table-column prop="name" label="名称 / 访问路径" min-width="240">
            <template #default="scope">
              <div class="project-identity">
                <span class="project-icon"><el-icon :size="19"><Monitor /></el-icon></span>
                <div class="project-text">
                  <router-link :to="`/web-share/${scope.row.id}`" class="project-name">{{ scope.row.name }}</router-link>
                  <div class="project-path">{{ scope.row.url }}</div><small v-if="scope.row.moderation_status && scope.row.moderation_status !== 'normal'" class="moderation-note">管理员已{{ scope.row.moderation_status === 'deleted' ? '删除' : '下架' }}：{{ scope.row.moderation_reason }}</small>
                </div>
              </div>
            </template>
          </el-table-column>
          <el-table-column prop="access_mode" label="可见范围" min-width="160">
            <template #default="scope"><span class="access-label">{{ accessModeText[scope.row.access_mode] || scope.row.access_mode }}</span></template>
          </el-table-column>
          <el-table-column prop="status" label="发布状态" width="110">
            <template #default="scope">
              <el-tag :type="statusTagType(scope.row.status)">{{ statusText[scope.row.status] || scope.row.status }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="update_time" label="更新时间" min-width="168" />
          <el-table-column fixed="right" label="操作" width="165" align="right">
            <template #default="scope">
              <el-button size="small" @click="viewProject(scope.row)">管理</el-button>
              <el-button v-if="scope.row.status === 'enabled' && (!scope.row.moderation_status || scope.row.moderation_status === 'normal')" size="small" type="primary" plain @click="openProject(scope.row)">打开</el-button>
            </template>
          </el-table-column>
          <template #empty>
            <el-empty :description="statusFilter === 'deleted' ? '回收站是空的' : '从你的第一个托管网页开始'" :image-size="90">
              <el-button v-if="statusFilter === 'active'" type="primary" plain @click="$router.push('/web-share/new')">新建托管</el-button>
            </el-empty>
          </template>
        </el-table>

        <div v-loading="loading" class="mobile-projects">
          <article v-for="project in projects" :key="project.id" class="mobile-project">
            <div class="project-identity">
              <span class="project-icon"><el-icon :size="19"><Monitor /></el-icon></span>
              <div class="project-text">
                <router-link :to="`/web-share/${project.id}`" class="project-name">{{ project.name }}</router-link>
                <div class="project-path">{{ project.url }}</div><small v-if="project.moderation_status && project.moderation_status !== 'normal'" class="moderation-note">管理员已{{ project.moderation_status === 'deleted' ? '删除' : '下架' }}：{{ project.moderation_reason }}</small>
              </div>
            </div>
            <div class="mobile-project-meta"><span>{{ accessModeText[project.access_mode] || project.access_mode }}</span><el-tag :type="statusTagType(project.status)">{{ statusText[project.status] || project.status }}</el-tag></div>
            <div class="mobile-project-actions">
              <el-button size="small" @click="viewProject(project)">管理</el-button>
              <el-button v-if="project.status === 'enabled' && (!project.moderation_status || project.moderation_status === 'normal')" size="small" type="primary" plain @click="openProject(project)">打开</el-button>
            </div>
          </article>
          <el-empty v-if="!loading && projects.length === 0" :description="statusFilter === 'deleted' ? '回收站是空的' : '还没有托管网页，点击上方新建'" :image-size="80" />
        </div>
        <div v-if="hasMore" class="load-more"><el-button :loading="loadingMore" @click="loadMore">加载更多</el-button></div>
      </section>
      <p class="project-list-note">支持 HTML 文档与静态网页 ZIP · 每项托管内容都可以单独设置访问范围</p>
    </main>
  </div>
</template>

<script>
import {ElMessage} from 'element-plus'
import {Monitor, Plus, Refresh} from '@element-plus/icons-vue'
import MyHeader from '@/components/MyHeader'

const {webShareApi} = require('@/api/web_projects.cjs')

export default {
  name: 'WebShareList',
  components: {MyHeader, Monitor, Plus, Refresh},
  data() {
    return {
      loading: false,
      loadingMore: false,
      projects: [],
      nextCursor: '',
      hasMore: false,
      statusFilter: 'active',
      accessModeText: {
        owner: '仅自己可见',
        members: '指定用户可见',
        authenticated: '所有登录用户可见',
        public: '所有人可见',
      },
      statusText: {
        draft: '草稿',
        enabled: '已发布',
        disabled: '已下线',
        deleted: '已删除',
      },
    }
  },
  created() {
    if (!this.requireLogin()) {
      this.loadProjects(true)
    }
  },
  methods: {
    requireLogin() {
      if (this.$store.state.userInfo) {
        return false
      }
      this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
      return true
    },
    statusTagType(status) {
      return {enabled: 'success', disabled: 'warning', deleted: 'danger'}[status] || 'info'
    },
    async loadProjects(reset) {
      if (this.requireLogin()) {
        return
      }
      this.loading = true
      try {
        const params = {cursor: reset ? '' : this.nextCursor, limit: 20}
        if (this.statusFilter === 'deleted') {
          params.status = 'deleted'
        }
        const data = await webShareApi.listProjects(params)
        this.projects = reset ? data.items : this.projects.concat(data.items)
        this.nextCursor = data.next_cursor || ''
        this.hasMore = Boolean(data.has_more)
      } catch (error) {
        this.handleError(error)
      } finally {
        this.loading = false
        this.loadingMore = false
      }
    },
    loadMore() {
      this.loadingMore = true
      this.loadProjects(false)
    },
    viewProject(project) {
      this.$router.push(`/web-share/${project.id}`)
    },
    async openProject(project) {
      try {
        await webShareApi.checkBrowserSession()
        window.location.assign(project.url)
      } catch (error) {
        this.handleError(error)
      }
    },
    handleError(error) {
      if (error.status === 401) {
        this.$store.commit('REMOVE_INFO')
        this.requireLogin()
        return
      }
      ElMessage.error(error.message || '网页托管内容加载失败')
    },
  },
}
</script>

<style scoped>
.moderation-note {color:#a35539;font-size:12px;}
.project-list-actions { align-items: center; margin-bottom: 28px; }
.project-list-actions .page-title { font-size: 30px; margin-bottom: 10px; }
.page-desc { margin: 0; color: var(--text-secondary); font-size: 14px; }
.projects-card { padding: 0 24px 8px; overflow: hidden; }
.list-filter { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 22px 0; }
.list-filter :deep(.el-radio-button__inner) { box-shadow: none; }
.project-identity { display: flex; align-items: center; gap: 12px; }
.project-icon { display: grid; place-items: center; width: 40px; height: 40px; flex-shrink: 0; border: 1px solid #e5ebe6; border-radius: 11px; background: #f4f8f4; color: #668b77; }
.project-text { min-width: 0; }
.project-name { color: var(--text-primary); font-weight: 600; text-decoration: none; overflow-wrap: anywhere; }
.project-name:hover { color: var(--primary-color); }
.project-path { margin-top: 3px; color: var(--text-secondary); font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; overflow-wrap: anywhere; }
.access-label { font-size: 13px; color: #5c7268; }
.load-more { padding: 20px 0; text-align: center; }
.project-list-note { color: var(--text-secondary); font-size: 12px; margin: 18px 2px 0; }
.mobile-projects { display: none; }
.mobile-project { border-top: 1px solid var(--border-color); padding: 20px 0; }
.mobile-project-meta { display: flex; justify-content: space-between; align-items: center; gap: 12px; margin-top: 18px; color: var(--text-secondary); font-size: 12px; }
.mobile-project-actions { display: flex; justify-content: flex-end; margin-top: 14px; }
@media (max-width: 720px) {
  .desktop-projects { display: none; }
  .mobile-projects { display: block; }
  .projects-card { padding: 0 18px; }
  .project-list-actions { align-items: flex-start; }
  .project-list-actions .page-title { font-size: 26px; }
}
@media (max-width: 480px) {
  .project-list-actions { flex-direction: column; gap: 18px; }
  .project-list-actions > .el-button { width: 100%; }
}
</style>
