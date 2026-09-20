<template>
  <div>
    <MyHeader />
    <main class="page-container manuals-page">
      <div class="action-bar manuals-heading">
        <div>
          <span class="page-eyebrow">HOME MANUALS</span>
          <h1 class="page-title">家庭说明书</h1>
          <p>把照片、PDF、文字和官网链接按顺序收在一起。</p>
        </div>
        <el-button type="primary" size="large" @click="createManual">新建说明书</el-button>
      </div>

      <form class="card manual-filters" aria-label="说明书筛选" @submit.prevent="search">
        <label>
          <span>名称</span>
          <input v-model="filters.q" aria-label="按名称搜索" maxlength="120" placeholder="输入名称关键词">
        </label>
        <label>
          <span>分类</span>
          <select v-model="filters.category" aria-label="分类筛选">
            <option :value="null">全部分类</option>
            <option value="">未分类</option>
            <option v-for="category in categories" :key="category" :value="category">{{ category }}</option>
          </select>
        </label>
        <label v-if="user" class="mine-filter">
          <input v-model="filters.mine" type="checkbox" aria-label="只看我的说明书">
          <span>只看我的</span>
        </label>
        <el-button native-type="submit" type="primary" :loading="loading">搜索</el-button>
      </form>

      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="manual-alert" />
      <section v-loading="loading && items.length === 0" class="manual-grid" aria-live="polite">
        <router-link v-for="manual in items" :key="manual.id" :to="`/manuals/${manual.id}`" class="manual-card">
          <div class="manual-cover" :class="`cover-${manual.cover?.kind || 'empty'}`">
            <img v-if="manual.cover?.thumbnail_url" :src="manual.cover.thumbnail_url" :alt="`${manual.name}封面`">
            <div v-else class="cover-summary">
              <strong>{{ coverKind(manual.cover) }}</strong>
              <span v-if="manual.cover?.title">{{ manual.cover.title }}</span>
              <p v-if="manual.cover?.text_excerpt">{{ manual.cover.text_excerpt }}</p>
              <p v-else-if="manual.cover?.url_host">{{ manual.cover.url_host }}</p>
              <small v-if="['image', 'pdf'].includes(manual.cover?.kind) && manual.cover?.preview_status === 'unavailable'">预览不可用，原件仍可查看</small>
            </div>
          </div>
          <div class="manual-card-body">
            <div v-if="manual.cover?.thumbnail_url" class="cover-caption"><span>{{ coverKind(manual.cover) }}</span><strong>{{ manual.cover.title || '封面资料' }}</strong></div>
            <div class="manual-card-title"><h2>{{ manual.name }}</h2><div class="manual-badges"><span v-if="manual.password_protected">需密码</span><span>{{ accessText(manual.access_mode) }}</span></div></div>
            <p v-if="manual.description" class="manual-description">{{ manual.description }}</p>
            <div class="manual-meta">
              <span v-if="!manual.categories?.length">未分类</span>
              <span v-for="category in manual.categories" :key="category">{{ category }}</span>
              <span>{{ manual.item_count || 0 }} 项资料</span>
              <span v-if="manual.update_time">更新于 {{ formatDate(manual.update_time) }}</span>
              <span v-if="manual.status === 'draft'">草稿</span>
            </div>
          </div>
        </router-link>
      </section>
      <el-empty v-if="!loading && !error && items.length === 0" description="没有找到说明书" />
      <div v-if="hasMore" class="load-more"><el-button :loading="loadingMore" @click="loadMore">加载更多</el-button></div>
    </main>
  </div>
</template>

<script>
import MyHeader from '@/components/MyHeader.vue'

const {manualsApi} = require('@/api/manuals.cjs')

export default {
  name: 'ManualList',
  components: {MyHeader},
  data() {
    return {
      filters: {q: '', category: null, mine: false},
      categories: [], items: [], nextCursor: '', hasMore: false,
      loading: false, loadingMore: false, error: '',
    }
  },
  computed: {
    user() { return this.$store.state.userInfo },
  },
  created() { this.load(true) },
  methods: {
    async load(reset) {
      this.loading = true
      this.error = ''
      try {
        await this.loadManuals(reset)
        await this.loadCategories()
      } catch (error) {
        this.error = error.message || '说明书加载失败'
      } finally {
        this.loading = false
        this.loadingMore = false
      }
    },
    async loadManuals(reset) {
      const params = {limit: 20}
      const keyword = this.filters.q.trim()
      if (keyword) params.q = keyword
      if (this.filters.category !== null) params.category = this.filters.category
      if (this.filters.mine) params.mine = true
      if (!reset && this.nextCursor) params.cursor = this.nextCursor
      const data = await manualsApi.listManuals(params)
      this.items = reset ? data.items || [] : this.items.concat(data.items || [])
      this.nextCursor = data.next_cursor || ''
      this.hasMore = Boolean(data.has_more)
    },
    async loadCategories() {
      const params = this.filters.mine ? {mine: true} : {}
      const data = await manualsApi.listAllCategories(params)
      this.categories = [...new Set((data.items || []).filter(Boolean))]
    },
    search() { this.load(true) },
    loadMore() { this.loadingMore = true; this.load(false) },
    createManual() {
      if (this.user) this.$router.push('/manuals/new')
      else this.$router.push({path: '/login', query: {redirect: '/manuals/new'}})
    },
    formatDate(value) {
      const date = new Date(value)
      return Number.isNaN(date.getTime()) ? '未知时间' : date.toLocaleDateString('zh-CN')
    },
    accessText(mode) { return {owner: '仅自己', authenticated: '登录可见', public: '公开'}[mode] || mode },
    coverKind(cover) { return {image: '图片', pdf: 'PDF', text: '文本', url: '网页'}[cover && cover.kind] || '暂无封面' },
  },
}
</script>

<style scoped>
.manuals-heading p {margin:0;color:var(--text-secondary);font-size:14px}.manual-filters{display:grid;grid-template-columns:minmax(180px,1fr) minmax(150px,.55fr) auto auto;align-items:end;gap:16px;margin-bottom:24px;padding:20px}.manual-filters label>span{display:block;margin-bottom:7px;color:var(--text-secondary);font-size:13px;font-weight:600}.manual-filters input[type=text],.manual-filters input:not([type]),.manual-filters select{width:100%;min-height:44px;border:1px solid var(--border-color);border-radius:9px;background:#fcfdfc;color:var(--text-primary);font:inherit;padding:0 12px}.manual-filters .mine-filter{display:flex;align-items:center;gap:8px;min-height:44px}.manual-filters .mine-filter span{margin:0;white-space:nowrap}.mine-filter input{width:18px;height:18px}.manual-alert{margin-bottom:20px}.manual-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:20px;min-height:120px}.manual-card{display:flex;min-width:0;flex-direction:column;overflow:hidden;border:1px solid var(--border-color);border-radius:15px;background:var(--card-bg);box-shadow:var(--card-shadow);color:inherit;text-decoration:none;transition:transform .18s,border-color .18s}.manual-card:hover{transform:translateY(-2px);border-color:#a8c7ba}.manual-cover{display:flex;min-height:170px;background:#e8f0e9;overflow:hidden}.manual-cover img{width:100%;height:190px;object-fit:cover}.cover-summary{display:flex;width:100%;min-width:0;flex-direction:column;justify-content:center;padding:24px;overflow-wrap:anywhere}.cover-summary strong{color:var(--primary-color);font-size:12px;letter-spacing:1px}.cover-summary span{margin-top:8px;font-weight:650}.cover-summary p{display:-webkit-box;overflow:hidden;margin:8px 0 0;color:var(--text-secondary);font-size:13px;line-height:1.7;-webkit-box-orient:vertical;-webkit-line-clamp:3}.cover-summary small{margin-top:9px;color:#996b2f}.cover-url{background:#eef2e7}.cover-pdf{background:#f3eee6}.manual-card-body{padding:18px}.cover-caption{display:flex;align-items:center;gap:8px;margin-bottom:10px;color:var(--text-secondary);font-size:12px}.cover-caption span{border-radius:5px;background:#edf4ef;color:var(--primary-dark);padding:2px 6px}.cover-caption strong{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.manual-card-title{display:flex;align-items:flex-start;justify-content:space-between;gap:10px}.manual-card h2{min-width:0;margin:0;font-size:18px;overflow-wrap:anywhere}.manual-badges{display:flex;flex-shrink:0;flex-wrap:wrap;justify-content:flex-end;gap:4px}.manual-badges>span{border-radius:999px;background:#edf5f0;color:var(--primary-dark);font-size:11px;padding:3px 8px}.manual-badges>span:first-child:not(:last-child){background:#f5eee1;color:#8a6432}.manual-description{display:-webkit-box;overflow:hidden;margin:10px 0;color:var(--text-secondary);font-size:13px;-webkit-box-orient:vertical;-webkit-line-clamp:2}.manual-meta{display:flex;flex-wrap:wrap;gap:8px 14px;margin-top:14px;color:var(--text-secondary);font-size:12px}.load-more{display:flex;justify-content:center;margin-top:24px}@media(max-width:900px){.manual-grid{grid-template-columns:repeat(2,minmax(0,1fr))}.manual-filters{grid-template-columns:1fr 1fr}.mine-filter{grid-column:1/2}}@media(max-width:560px){.manual-grid{grid-template-columns:1fr}.manual-filters{grid-template-columns:minmax(0,1fr);padding:16px}.mine-filter{grid-column:auto}.manual-cover{min-height:135px}.manual-cover img{height:160px}.manuals-heading{align-items:stretch}.manuals-heading .el-button{width:100%;min-height:44px}}
</style>
