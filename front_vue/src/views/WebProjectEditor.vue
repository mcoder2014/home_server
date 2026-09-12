<template>
  <div>
    <MyHeader />
    <main class="page-container project-editor" :class="{'is-create': isCreate}">
      <div class="editor-heading">
        <el-button text @click="$router.push('/web-projects')">
          <el-icon><ArrowLeft /></el-icon>
          返回列表
        </el-button>
        <div v-if="project.id" class="heading-status">
          <el-tag :type="statusTagType(project.status)">{{ statusText[project.status] || project.status }}</el-tag>
        </div>
      </div>

      <div class="project-page-heading">
        <span class="page-eyebrow">WEB PROJECTS</span>
        <h1>{{ isCreate ? '新建网页项目' : project.name || '项目设置' }}</h1>
        <p>{{ isCreate ? '给它一个名字和地址，再选择谁可以访问。' : '管理项目的访问范围、网页内容与发布版本。' }}</p>
      </div>
      <el-row :gutter="24">
        <el-col :xs="24" :lg="isCreate ? 24 : 14">
          <section class="card editor-section">
            <h2 class="page-title">基本信息</h2>
            <p class="section-desc">项目地址只使用小写字母、数字和连字符，修改后旧地址立即失效。</p>

            <el-form ref="projectForm" :model="form" :rules="rules" label-position="top">
              <el-form-item label="项目名称" prop="name">
                <el-input v-model="form.name" maxlength="256" show-word-limit />
              </el-form-item>
              <el-form-item label="项目说明" prop="description">
                <el-input v-model="form.description" type="textarea" :rows="3" maxlength="500" show-word-limit />
              </el-form-item>
              <el-form-item label="项目路径" prop="slug">
                <el-input v-model="form.slug" maxlength="256">
                  <template #prepend>/p/</template>
                  <template #append>/</template>
                </el-input>
              </el-form-item>
              <el-form-item label="可见范围" prop="access_mode">
                <el-radio-group v-model="form.access_mode" class="access-options">
                  <el-radio label="owner">仅自己可见</el-radio>
                  <el-radio label="members">指定用户可见</el-radio>
                  <el-radio label="authenticated">所有登录用户可见</el-radio>
                  <el-radio label="public">所有人可见</el-radio>
                </el-radio-group>
              </el-form-item>
              <el-alert
                v-if="form.access_mode === 'public'"
                title="任何人都能访问项目内的 HTML、脚本、图片和附件。"
                type="warning"
                :closable="false"
                show-icon
                class="form-alert"
              />
              <el-form-item v-if="form.access_mode === 'members'" label="可访问用户" prop="member_user_ids">
                <el-select
                  v-model="form.member_user_ids"
                  multiple
                  filterable
                  clearable
                  placeholder="选择已有账号；创建者始终可访问"
                  class="full-width"
                >
                  <el-option v-for="user in eligibleUsers" :key="user.id" :label="user.user_name" :value="user.id" />
                </el-select>
              </el-form-item>
              <div class="form-actions">
                <el-button type="primary" :loading="saving" @click="saveProject">
                  {{ isCreate ? '创建项目' : '保存设置' }}
                </el-button>
                <template v-if="!isCreate">
                  <el-button v-if="project.status === 'enabled'" :loading="mutating" @click="disableProject">下线</el-button>
                  <el-button v-if="project.status !== 'deleted'" type="danger" plain :loading="mutating" @click="deleteProject">删除</el-button>
                  <el-button v-else type="primary" plain :loading="mutating" @click="restoreProject">恢复项目</el-button>
                </template>
              </div>
            </el-form>
          </section>

          <section v-if="!isCreate && project.status !== 'deleted'" class="card editor-section">
            <div class="section-title-row">
              <div>
                <h2 class="page-title">上传版本</h2>
                <p class="section-desc">HTML 会保存为 index.html；ZIP 根目录应直接包含静态产物。</p>
              </div>
              <el-tag v-if="project.access_mode === 'public'" type="warning">当前已公开</el-tag>
            </div>
            <el-alert
              v-if="accessSettingsDirty"
              title="可见范围或用户名单尚未保存。请先保存设置，再发布版本。"
              type="warning"
              :closable="false"
              show-icon
              class="form-alert"
            />
            <el-upload
              v-model:file-list="uploadFileList"
              drag
              action="#"
              accept=".html,.htm,.zip,text/html,application/zip"
              :auto-upload="false"
              :limit="1"
              :on-change="handleFileChange"
              :on-remove="handleFileRemove"
              :on-exceed="handleFileExceed"
            >
              <el-icon class="el-icon--upload"><UploadFilled /></el-icon>
              <div class="el-upload__text">拖入 HTML 或 ZIP，或<em>点击选择</em></div>
              <template #tip>
                <div class="el-upload__tip">上传包最大 50 MiB；ZIP 默认入口为 index.html。</div>
              </template>
            </el-upload>
            <el-form-item label="ZIP 入口文件（可选）" class="entry-file-field">
              <el-input v-model="entryFile" maxlength="2048" placeholder="index.html" show-word-limit />
            </el-form-item>
            <div class="form-actions">
              <el-button :disabled="!uploadFile" :loading="uploading" @click="uploadRelease(false)">仅上传版本</el-button>
              <el-button type="primary" :disabled="!uploadFile || accessSettingsDirty" :loading="uploading" @click="uploadRelease(true)">上传并发布</el-button>
            </div>
          </section>
        </el-col>

        <el-col v-if="!isCreate" :xs="24" :lg="10">
          <section class="card editor-section project-overview">
            <h2 class="page-title">访问地址</h2>
            <div class="project-url">{{ absoluteProjectUrl }}</div>
            <div class="form-actions">
              <el-button @click="copyProjectUrl">复制链接</el-button>
              <el-button v-if="project.status === 'enabled'" type="primary" @click="openProject">打开项目</el-button>
            </div>
            <el-alert
              v-if="project.status !== 'enabled'"
              title="草稿、已下线或已删除的项目不能从正式地址访问。"
              type="info"
              :closable="false"
              show-icon
            />
          </section>

          <section class="card editor-section">
            <div class="section-title-row">
              <div>
                <h2 class="page-title">发布历史</h2>
                <p class="section-desc">发布旧版本就是回滚，项目 URL 和可见范围不会改变。</p>
              </div>
              <el-button :loading="loadingReleases" aria-label="刷新发布历史" circle @click="loadReleases(true)">
                <el-icon><Refresh /></el-icon>
              </el-button>
            </div>
            <el-empty v-if="!loadingReleases && releases.length === 0" description="暂无版本" />
            <div v-loading="loadingReleases" class="release-list">
              <div v-for="release in releases" :key="release.id" class="release-item">
                <div class="release-main">
                  <div class="release-id">
                    版本 {{ release.id }}
                    <el-tag v-if="release.id === project.current_release_id" size="small" type="success">当前</el-tag>
                    <el-tag v-else size="small" :type="release.status === 'ready' ? 'info' : 'danger'">{{ release.status }}</el-tag>
                  </div>
                  <div class="release-meta">{{ release.file_count }} 个文件 · {{ formatBytes(release.total_bytes) }}</div>
                  <div class="release-meta">{{ release.create_time }}</div>
                  <div v-if="release.error_message" class="release-error">{{ release.error_message }}</div>
                </div>
                <div class="release-actions">
                  <el-button size="small" @click="downloadRelease(release)">下载</el-button>
                  <el-button
                    v-if="canPublish(release)"
                    size="small"
                    type="primary"
                    plain
                    :disabled="accessSettingsDirty"
                    @click="publishRelease(release)"
                  >{{ publishActionText(release) }}</el-button>
                </div>
              </div>
            </div>
            <div v-if="releaseHasMore" class="load-more">
              <el-button :loading="loadingReleases" @click="loadReleases(false)">加载更多</el-button>
            </div>
          </section>
        </el-col>
      </el-row>
    </main>
  </div>
</template>

<script>
import {ElMessage, ElMessageBox} from 'element-plus'
import {ArrowLeft, Refresh, UploadFilled} from '@element-plus/icons-vue'
import MyHeader from '@/components/MyHeader'

const {webProjectsApi} = require('@/api/web_projects.cjs')
const {canPublishRelease, hasUnsavedAccessChanges} = require('@/utils/web_projects_behavior.cjs')

export default {
  name: 'WebProjectEditor',
  components: {MyHeader, ArrowLeft, Refresh, UploadFilled},
  data() {
    return {
      saving: false,
      mutating: false,
      uploading: false,
      loadingReleases: false,
      project: {},
      form: {
        name: '',
        description: '',
        slug: '',
        access_mode: 'owner',
        member_user_ids: [],
      },
      rules: {
        name: [{required: true, message: '请输入项目名称', trigger: 'blur'}],
        slug: [
          {required: true, message: '请输入相对 URL', trigger: 'blur'},
          {pattern: /^[a-z0-9](?:[a-z0-9-]{1,254}[a-z0-9])$/, message: '请输入 3～256 位小写字母、数字或连字符，首尾不能是连字符', trigger: 'blur'},
        ],
      },
      eligibleUsers: [],
      releases: [],
      releaseCursor: '',
      releaseHasMore: false,
      uploadFile: null,
      uploadFileList: [],
      entryFile: '',
      statusText: {
        draft: '草稿',
        enabled: '已发布',
        disabled: '已下线',
        deleted: '已删除',
      },
    }
  },
  computed: {
    isCreate() {
      return this.$route.name === 'WebProjectCreate'
    },
    absoluteProjectUrl() {
      if (!this.project.url) {
        return ''
      }
      return `${window.location.origin}${this.project.url}`
    },
    accessSettingsDirty() {
      return Boolean(this.project.id) && hasUnsavedAccessChanges(this.project, this.form)
    },
  },
  watch: {
    project(project) {
      // 仅让当前详情更新标题，离开页面后的异步响应不能覆盖其他页面。
      if (this.$route.name === 'WebProjectDetail' && String(project.id) === this.$route.params.id) {
        document.title = `CQ Home Server · ${project.name}`
      }
    },
  },
  async created() {
    if (!localStorage.getItem('token')) {
      this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
      return
    }
    await this.loadEligibleUsers()
    if (!this.isCreate) {
      await Promise.all([this.loadProject(), this.loadReleases(true)])
    }
  },
  methods: {
    statusTagType(status) {
      return {enabled: 'success', disabled: 'warning', deleted: 'danger'}[status] || 'info'
    },
    applyProject(project) {
      this.project = project
      this.form = {
        name: project.name,
        description: project.description || '',
        slug: project.slug,
        access_mode: project.access_mode,
        member_user_ids: project.member_user_ids || [],
      }
    },
    async loadProject() {
      try {
        this.applyProject(await webProjectsApi.getProject(this.$route.params.id))
      } catch (error) {
        this.handleError(error)
      }
    },
    async loadEligibleUsers() {
      try {
        const data = await webProjectsApi.listEligibleUsers()
        this.eligibleUsers = data.items || []
      } catch (error) {
        this.handleError(error)
      }
    },
    async loadReleases(reset) {
      this.loadingReleases = true
      try {
        const data = await webProjectsApi.listReleases(this.$route.params.id, {
          cursor: reset ? '' : this.releaseCursor,
          limit: 20,
        })
        this.releases = reset ? data.items : this.releases.concat(data.items)
        this.releaseCursor = data.next_cursor || ''
        this.releaseHasMore = Boolean(data.has_more)
      } catch (error) {
        this.handleError(error)
      } finally {
        this.loadingReleases = false
      }
    },
    projectPayload() {
      return {
        name: this.form.name.trim(),
        description: this.form.description.trim(),
        slug: this.form.slug.trim(),
        access_mode: this.form.access_mode,
        member_user_ids: this.form.access_mode === 'members' ? this.form.member_user_ids : [],
      }
    },
    async saveProject() {
      try {
        await this.$refs.projectForm.validate()
      } catch (error) {
        return
      }
      this.saving = true
      try {
        const project = this.isCreate
          ? await webProjectsApi.createProject(this.projectPayload())
          : await webProjectsApi.updateProject(this.project.id, this.project.revision, this.projectPayload())
        ElMessage.success(this.isCreate ? '项目已创建' : '项目设置已保存')
        if (this.isCreate) {
          await this.$router.replace(`/web-projects/${project.id}`)
          this.applyProject(project)
          await this.loadReleases(true)
        } else {
          this.applyProject(project)
        }
      } catch (error) {
        this.handleError(error)
      } finally {
        this.saving = false
      }
    },
    handleFileChange(file) {
      this.uploadFile = file.raw
    },
    handleFileRemove() {
      this.uploadFile = null
    },
    handleFileExceed() {
      ElMessage.warning('一次只能选择一个 HTML 或 ZIP 文件')
    },
    async uploadRelease(publishNow) {
      if (!this.uploadFile) {
        return
      }
      if (publishNow && !(await this.confirmPublishAllowed())) {
        return
      }
      this.uploading = true
      try {
        const release = await webProjectsApi.uploadRelease(this.project.id, this.uploadFile, this.entryFile.trim())
        ElMessage.success('版本上传完成')
        if (publishNow) {
          this.applyProject(await webProjectsApi.publishRelease(this.project.id, this.project.revision, release.id))
          ElMessage.success('新版本已发布')
        }
        this.uploadFile = null
        this.uploadFileList = []
        await this.loadReleases(true)
      } catch (error) {
        this.handleError(error)
      } finally {
        this.uploading = false
      }
    },
    async publishRelease(release) {
      if (!(await this.confirmPublishAllowed())) {
        return
      }
      this.mutating = true
      try {
        this.applyProject(await webProjectsApi.publishRelease(this.project.id, this.project.revision, release.id))
        ElMessage.success('版本已发布')
      } catch (error) {
        this.handleError(error)
      } finally {
        this.mutating = false
      }
    },
    async disableProject() {
      try {
        await ElMessageBox.confirm('下线后正式地址立即返回 404，文件和版本仍会保留。', '确认下线', {type: 'warning'})
      } catch (error) {
        return
      }
      await this.runProjectMutation(() => webProjectsApi.disableProject(this.project.id, this.project.revision), '项目已下线')
    },
    async deleteProject() {
      try {
        await ElMessageBox.confirm('删除后项目进入回收期并立即停止访问，原路径不会自动释放。需要复用该路径时，请先修改项目路径再删除。', '确认删除', {type: 'warning'})
      } catch (error) {
        return
      }
      await this.runProjectMutation(() => webProjectsApi.deleteProject(this.project.id, this.project.revision), '项目已删除')
    },
    async restoreProject() {
      await this.runProjectMutation(() => webProjectsApi.restoreProject(this.project.id, this.project.revision), '项目已恢复为下线状态')
    },
    async runProjectMutation(action, successMessage) {
      this.mutating = true
      try {
        this.applyProject(await action())
        ElMessage.success(successMessage)
      } catch (error) {
        this.handleError(error)
      } finally {
        this.mutating = false
      }
    },
    canPublish(release) {
      return canPublishRelease(this.project, release)
    },
    publishActionText(release) {
      if (release.id === this.project.current_release_id && this.project.status !== 'enabled') {
        return '重新上线'
      }
      return this.project.current_release_id ? '回滚至此版本' : '发布'
    },
    async confirmPublishAllowed() {
      if (this.accessSettingsDirty) {
        ElMessage.warning('请先保存可见范围和用户名单')
        return false
      }
      if (this.project.access_mode !== 'public') {
        return true
      }
      try {
        await ElMessageBox.confirm('当前已保存的可见范围是“所有人可见”，发布后无需登录即可访问。', '确认公开发布', {type: 'warning'})
        return true
      } catch (error) {
        return false
      }
    },
    async openProject() {
      try {
        await webProjectsApi.createBrowserLogin()
        window.location.assign(this.project.url)
      } catch (error) {
        this.handleError(error)
      }
    },
    async copyProjectUrl() {
      try {
        await navigator.clipboard.writeText(this.absoluteProjectUrl)
        ElMessage.success('链接已复制')
      } catch (error) {
        ElMessage.error('复制失败，请手动复制地址')
      }
    },
    async downloadRelease(release) {
      try {
        const response = await webProjectsApi.downloadRelease(this.project.id, release.id)
        const url = URL.createObjectURL(response.data)
        const link = document.createElement('a')
        link.href = url
        link.download = `${this.project.slug}-release-${release.id}.zip`
        link.click()
        URL.revokeObjectURL(url)
      } catch (error) {
        this.handleError(error)
      }
    },
    formatBytes(bytes) {
      const value = Number(bytes || 0)
      if (value < 1024) {
        return `${value} B`
      }
      if (value < 1024 * 1024) {
        return `${(value / 1024).toFixed(1)} KiB`
      }
      return `${(value / 1024 / 1024).toFixed(1)} MiB`
    },
    handleError(error) {
      if (error.status === 401) {
        localStorage.removeItem('token')
        this.$router.replace({path: '/login', query: {redirect: this.$route.fullPath}})
        return
      }
      if (error.status === 409) {
        ElMessage.error('项目已被其他操作修改，正在刷新最新数据')
        if (!this.isCreate) {
          this.loadProject()
        }
        return
      }
      ElMessage.error(error.message || '操作失败')
    },
  },
}
</script>

<style scoped>
.project-editor.is-create { max-width: 820px; }
.project-page-heading { margin: 16px 0 28px; }
.project-page-heading h1 { margin: 0 0 10px; font-size: 30px; font-weight: 650; letter-spacing: -0.5px; overflow-wrap: anywhere; }
.project-page-heading p { margin: 0; color: var(--text-secondary); font-size: 14px; }
.editor-section .page-title { font-size: 19px; }
.access-options { width: 100%; grid-template-columns: repeat(2, minmax(0, 1fr)); }
.access-options :deep(.el-radio) { margin: 0; padding: 12px; height: auto; min-height: 44px; border: 1px solid var(--border-color); border-radius: 8px; }
.access-options :deep(.el-radio.is-checked) { border-color: #94b9a7; background: #f0f6f1; }
.access-options :deep(.el-radio__label) { white-space: normal; font-size: 13px; }
.form-actions > .el-button, .release-actions > .el-button { margin-left: 0; }
@media (max-width: 600px) {
  .project-page-heading h1 { font-size: 25px; }
  .access-options { grid-template-columns: 1fr; }
}

.project-editor {
  padding-top: 16px;
}

.editor-heading {
  align-items: center;
  display: flex;
  justify-content: space-between;
  margin-bottom: 12px;
}

.heading-status {
  align-items: center;
  color: var(--text-secondary);
  display: flex;
  font-size: 13px;
  gap: 10px;
}

.editor-section {
  margin-bottom: 20px;
}

.section-desc {
  color: var(--text-secondary);
  font-size: 14px;
  line-height: 1.6;
  margin: -8px 0 22px;
}

.section-title-row {
  align-items: flex-start;
  display: flex;
  justify-content: space-between;
}

.access-options {
  align-items: flex-start;
  display: grid;
  gap: 10px;
}

.form-alert {
  margin: -4px 0 20px;
}

.full-width {
  width: 100%;
}

.form-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 20px;
}

.entry-file-field {
  margin-top: 20px;
}

.project-url {
  background: #f6f9f6;
  border: 1px solid var(--border-color);
  border-radius: 9px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  overflow-wrap: anywhere;
  padding: 12px;
}

.release-list {
  min-height: 40px;
}

.release-item {
  border-top: 1px solid var(--border-color);
  display: flex;
  gap: 12px;
  justify-content: space-between;
  padding: 16px 0;
}

.release-id {
  align-items: center;
  display: flex;
  font-size: 14px;
  font-weight: 600;
  gap: 8px;
}

.release-meta {
  color: var(--text-secondary);
  font-size: 12px;
  margin-top: 5px;
}

.release-error {
  color: #f56c6c;
  font-size: 12px;
  margin-top: 5px;
}

.release-actions {
  align-items: flex-end;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.load-more {
  margin-top: 12px;
  text-align: center;
}

@media (max-width: 600px) {
  .release-item {
    flex-direction: column;
  }

  .release-actions {
    align-items: stretch;
    flex-direction: row;
  }
}
</style>
