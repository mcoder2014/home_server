<template>
  <div>
    <MyHeader />
    <main class="page-container manual-detail-page">
      <div v-if="loading" class="detail-loading" v-loading="true">正在加载说明书……</div>
      <el-result v-else-if="error" icon="error" title="无法查看说明书" :sub-title="error">
        <template #extra><el-button type="primary" @click="$router.push('/manuals')">返回列表</el-button></template>
      </el-result>
      <template v-else-if="manual">
        <header class="detail-heading">
          <div>
            <router-link to="/manuals" class="back-link">← 说明书列表</router-link>
            <span class="page-eyebrow">{{ manual.categories?.length ? manual.categories.join(' · ') : '未分类' }}</span>
            <h1>{{ manual.name }}</h1>
            <p v-if="manual.description">{{ manual.description }}</p>
            <div class="detail-meta"><span>{{ accessText(manual.access_mode) }}</span><span>{{ manual.item_count || manual.items?.length || 0 }} 项资料</span><span v-if="manual.status === 'draft'">草稿</span></div>
          </div>
          <el-button v-if="manual.can_edit" type="primary" size="large" @click="$router.push(`/manuals/${manual.id}/edit`)">编辑说明书</el-button>
        </header>

        <section class="manual-items" aria-label="说明书资料">
          <article v-for="(item, index) in orderedItems" :key="item.id" class="manual-item">
            <div class="item-heading"><span>{{ index + 1 }}</span><div><h2>{{ item.title || item.original_name || itemKind(item.kind) }}</h2><small>{{ itemKind(item.kind) }}</small></div></div>
            <el-image v-if="item.kind === 'image'" class="manual-image" :src="item.content_url" :preview-src-list="[item.content_url]" :alt="item.title || item.original_name || '说明书图片'" fit="contain" preview-teleported />
            <div v-else-if="item.kind === 'pdf'" class="pdf-actions">
              <p v-if="item.preview_status && item.preview_status !== 'ready'">首页预览不可用，PDF 原件仍可打开或下载。</p>
              <a :href="item.content_url" target="_blank" rel="noopener noreferrer" aria-label="打开 PDF">在新窗口打开 PDF</a>
              <a :href="downloadURL(item.content_url)" target="_blank" rel="noopener noreferrer" aria-label="下载 PDF">下载 PDF</a>
            </div>
            <pre v-else-if="item.kind === 'text'" class="manual-text">{{ item.text }}</pre>
            <div v-else-if="item.kind === 'url'" class="manual-url">
              <a v-if="safeURL(item.url)" :href="safeURL(item.url)" target="_blank" rel="noopener noreferrer" :aria-label="`打开${item.title || '网页链接'}`">{{ item.url }}</a>
              <p v-else>这条链接不是有效的 HTTP(S) 地址，已停止打开。</p>
            </div>
          </article>
          <el-empty v-if="orderedItems.length === 0" description="这份说明书还没有资料" />
        </section>
      </template>
    </main>
  </div>
</template>

<script>
import MyHeader from '@/components/MyHeader.vue'

const {manualsApi} = require('@/api/manuals.cjs')
const {safeExternalURL} = require('@/utils/manuals_behavior.cjs')

export default {
  name: 'ManualDetail',
  components: {MyHeader},
  data() { return {manual: null, loading: true, error: ''} },
  computed: {
    orderedItems() { return (this.manual?.items || []).slice().sort((a, b) => Number(a.position || 0) - Number(b.position || 0)) },
  },
  created() { this.loadManual() },
  methods: {
    async loadManual() {
      this.loading = true
      this.error = ''
      try { this.manual = await manualsApi.getManual(String(this.$route.params.id)) }
      catch (error) { this.error = error.message || '说明书加载失败' }
      finally { this.loading = false }
    },
    safeURL(value) { return safeExternalURL(value) },
    downloadURL(value) { return `${value}${String(value).includes('?') ? '&' : '?'}download=1` },
    itemKind(kind) { return {image: '图片', pdf: 'PDF', text: '文本', url: '网页链接'}[kind] || '资料' },
    accessText(mode) { return {owner: '仅自己可见', authenticated: '登录用户可见', public: '所有人可见'}[mode] || mode },
  },
}
</script>

<style scoped>
.manual-detail-page{max-width:920px}.detail-loading{min-height:240px;padding:80px;text-align:center;color:var(--text-secondary)}.detail-heading{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:30px;padding:8px 0}.back-link{display:inline-block;margin-bottom:24px;color:var(--text-secondary);font-size:13px;text-decoration:none}.detail-heading h1{margin:0;font-size:clamp(28px,5vw,42px);line-height:1.3;overflow-wrap:anywhere}.detail-heading p{max-width:680px;margin:14px 0;color:var(--text-secondary);white-space:pre-wrap}.detail-meta{display:flex;flex-wrap:wrap;gap:9px;margin-top:15px}.detail-meta span{border-radius:999px;background:#eaf2ed;color:var(--primary-dark);font-size:12px;padding:4px 10px}.manual-items{display:grid;gap:22px}.manual-item{min-width:0;overflow:hidden;border:1px solid var(--border-color);border-radius:16px;background:#fff;padding:24px}.item-heading{display:flex;align-items:flex-start;gap:13px;margin-bottom:18px}.item-heading>span{display:grid;width:28px;height:28px;flex:0 0 28px;place-items:center;border-radius:50%;background:var(--primary-darker);color:#fff;font-size:12px}.item-heading h2{margin:0;font-size:18px;overflow-wrap:anywhere}.item-heading small{color:var(--text-secondary)}.manual-image{display:block;width:100%;max-height:75vh;border-radius:10px;background:#f2f4f1}.manual-image :deep(img){max-height:75vh}.pdf-actions{display:flex;flex-wrap:wrap;gap:12px}.pdf-actions p{flex-basis:100%;margin:0;color:#996b2f}.pdf-actions a,.manual-url a{display:inline-flex;min-height:44px;align-items:center;border:1px solid #a9c5ba;border-radius:9px;padding:8px 14px;text-decoration:none;overflow-wrap:anywhere}.manual-text{max-width:100%;overflow:auto;margin:0;border-radius:10px;background:#f6f8f5;padding:18px;color:var(--text-primary);font:14px/1.8 ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap;overflow-wrap:anywhere}.manual-url{min-width:0}.manual-url a{max-width:100%}.manual-url p{margin:0;color:#9a503c}@media(max-width:600px){.detail-heading{align-items:stretch;flex-direction:column}.detail-heading .el-button{width:100%;min-height:44px}.manual-item{padding:18px 14px}.detail-loading{padding:70px 10px}.pdf-actions{flex-direction:column}.pdf-actions a{width:100%;justify-content:center}}
</style>
