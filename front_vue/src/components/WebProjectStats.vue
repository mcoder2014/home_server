<template>
  <section class="card project-stats" aria-labelledby="project-stats-title" :aria-busy="loading">
    <div class="stats-heading">
      <div>
        <span class="stats-eyebrow">TRAFFIC OVERVIEW</span>
        <h2 id="project-stats-title">访问统计</h2>
        <p>仅项目所有者可见 · 按上海时间自然日汇总</p>
      </div>
      <div class="stats-controls">
        <div class="range-options" role="group" aria-label="统计日期范围">
          <button v-for="range in [7, 30, 90]" :key="range" type="button" :aria-pressed="days === range" @click="days = range">{{ range }} 日</button>
        </div>
        <button class="stats-refresh" type="button" :disabled="loading" @click="loadStats">{{ loading ? '加载中…' : '刷新' }}</button>
      </div>
    </div>

    <p v-if="error" class="stats-notice is-warning" role="alert">{{ error }}</p>
    <p v-else-if="loading" class="stats-notice" role="status">正在读取已保存的统计…</p>
    <template v-else-if="model">
      <div class="stats-health" :class="{'is-warning': model.quality !== 'ok'}" role="status">
        <span class="health-dot" />
        <strong>{{ qualityText[model.quality] }}</strong>
        <span>{{ statusDescription }}</span>
      </div>
      <div class="stats-cards">
        <div class="metric"><span>累计 PV</span><strong>{{ formatCount(model.total.pv) }}</strong><small>统计启用以来的页面请求</small></div>
        <div class="metric"><span>累计 UV <em>近似</em></span><strong>{{ formatCount(model.total.uv) }}</strong><small>全历史浏览器访客去重</small></div>
        <div class="metric"><span>今日 PV</span><strong>{{ formatCount(model.today.pv) }}</strong><small>{{ qualityText[model.today.quality] }}</small></div>
        <div class="metric"><span>今日 UV <em>近似</em></span><strong>{{ formatCount(model.today.uv) }}</strong><small>{{ model.today.date }} · 上海时间</small></div>
      </div>
      <div class="trend-heading">
        <div><h3>每日访问趋势</h3><p>近 {{ days }} 日{{ model.periodPartial ? '已知' : '' }} PV <strong>{{ formatCount(model.periodPV) }}</strong><span v-if="model.periodPartial"> · 含数据缺口</span></p></div>
        <div class="chart-legend"><span class="pv-key">PV</span><span class="uv-key">UV（近似）</span><span class="gap-key">未采集 / 缺口</span></div>
      </div>
      <div class="chart-scroll">
        <svg viewBox="0 0 920 256" class="stats-chart" role="img" :aria-label="`近 ${days} 日每日 PV 和 UV 趋势；缺口不连接，精确数值见每日明细`">
          <line x1="52" x2="880" y1="32" y2="32" class="grid-line" />
          <line x1="52" x2="880" y1="124" y2="124" class="grid-line" />
          <line x1="52" x2="880" y1="216" y2="216" class="grid-line" />
          <text x="42" y="36" text-anchor="end">{{ formatCount(model.peak) }}</text>
          <text x="42" y="220" text-anchor="end">0</text>
          <g v-for="day in model.daily" :key="day.date">
            <rect v-if="day.quality !== 'ok'" :x="day.x - 3" y="32" width="6" height="184" class="gap-band"><title>{{ day.date }}：{{ qualityText[day.quality] }} {{ day.quality_reason }}</title></rect>
          </g>
          <path v-for="(line, index) in model.paths.pv" :key="`area-${index}`" :d="areaPath(line)" class="pv-area" />
          <path v-for="(line, index) in model.paths.pv" :key="`pv-${index}`" :d="line" class="pv-line" />
          <path v-for="(line, index) in model.paths.uv" :key="`uv-${index}`" :d="line" class="uv-line" />
          <g v-for="day in model.daily.filter(item => item.quality === 'ok')" :key="`point-${day.date}`">
            <circle :cx="day.x" :cy="day.pvY" r="3" class="pv-point"><title>{{ day.date }}：PV {{ day.pv }}</title></circle>
            <circle :cx="day.x" :cy="day.uvY" r="3" class="uv-point"><title>{{ day.date }}：UV ≈ {{ day.uv }}</title></circle>
          </g>
          <text v-for="day in model.ticks" :key="`tick-${day.date}`" :x="day.x" y="244" text-anchor="middle">{{ day.date.slice(5) }}</text>
        </svg>
      </div>
      <p class="chart-note">折线仅连接采集正常的日期；缺口不表示零访问。每日 UV 不能相加作为区间去重 UV。</p>
      <details class="daily-details">
        <summary>查看每日明细（{{ days }} 日）</summary>
        <div class="daily-table-wrap"><table>
          <thead><tr><th>日期（上海时间）</th><th>PV</th><th>UV（近似）</th><th>数据状态</th></tr></thead>
          <tbody><tr v-for="day in [...model.daily].reverse()" :key="day.date"><td>{{ day.date }}</td><td>{{ formatCount(day.pv) }}</td><td>{{ formatCount(day.uv) }}</td><td>{{ qualityText[day.quality] }}<span v-if="day.quality_reason"> · {{ day.quality_reason }}</span></td></tr></tbody>
        </table></div>
      </details>
      <div class="stats-footnote">
        <p>统计开始：{{ formatTime(stats.tracking_started_at) }}<span>最近持久化：{{ formatTime(stats.persisted_at) }}</span></p>
        <p>显示已保存的数据，正常约延迟 60 秒。仅统计成功取得 HTML 页面的请求；刷新计入 PV，SPA 内部切换不计入。</p>
        <p>UV 按浏览器标识近似去重，不等于真实人数。清除 Cookie、换浏览器或设备会成为新访客；内外网不同 hostname 不共享标识，同一浏览器可能重复计入 UV。</p>
      </div>
    </template>
  </section>
</template>

<script>
const {webShareApi} = require('@/api/web_projects.cjs')
const {buildStatsView, qualityText} = require('@/utils/web_project_stats.cjs')

export default {
  name: 'WebProjectStats',
  props: {projectId: {type: String, required: true}},
  data() {
    return {days: 30, stats: null, loading: false, error: '', requestSequence: 0, qualityText}
  },
  computed: {
    model() {
      return this.stats ? buildStatsView(this.stats, this.days) : null
    },
    statusDescription() {
      const descriptions = {
        ok: '累计数据自统计启用时开始记录。', disabled: '采集已停用，历史快照仍保留。',
        not_started: '尚无统计快照，启用前的历史访问不会补录。',
        degraded: '采集曾中断，已知计数可能低于实际访问量。', unknown: '部分数据状态未知，请结合每日明细查看缺口。',
      }
      return this.stats.quality_reason || descriptions[this.model.quality]
    },
  },
  watch: {
    projectId: {immediate: true, handler: 'loadStats'},
    days: 'loadStats',
  },
  beforeUnmount() {
    this.requestSequence += 1
  },
  methods: {
    // 日期或项目切换时丢弃旧请求，失败只更新此面板，不阻断编辑、上传或发布。
    async loadStats() {
      const sequence = ++this.requestSequence
      const projectId = this.projectId
      const days = this.days
      this.stats = null
      this.error = ''
      this.loading = Boolean(projectId)
      if (!projectId) return
      try {
        const stats = await webShareApi.getStats(projectId, days)
        if (sequence !== this.requestSequence || projectId !== this.projectId || days !== this.days) return
        if (!stats || stats.project_id !== projectId) throw new Error('统计响应的项目不匹配，请刷新重试')
        this.stats = stats
      } catch (error) {
        if (sequence !== this.requestSequence || projectId !== this.projectId || days !== this.days) return
        this.error = [403, 404].includes(error.status) ? '无权查看此项目统计，或项目不存在；统计仅对所有者开放。' : (error.message || '统计加载失败，请刷新重试。')
      } finally {
        if (sequence === this.requestSequence) this.loading = false
      }
    },
    formatCount(value) {
      if (value == null) return '—'
      return value.toLocaleString('zh-CN')
    },
    formatTime(value) {
      if (!value) return '暂无'
      const date = new Date(value)
      return Number.isFinite(date.getTime()) ? new Intl.DateTimeFormat('zh-CN', {timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false}).format(date) : '暂无'
    },
    areaPath(line) {
      const points = line.split(' ')
      const firstX = points[0].slice(1).split(',')[0]
      const lastX = points[points.length - 1].slice(1).split(',')[0]
      return `${line} L${lastX},216 L${firstX},216 Z`
    },
  },
}
</script>

<style scoped>
.project-stats { margin: 4px 0 24px; }
.stats-heading, .stats-controls, .trend-heading, .chart-legend { display: flex; align-items: center; justify-content: space-between; gap: 16px; }
.stats-eyebrow { color: #587965; font-size: 11px; letter-spacing: 1.5px; font-weight: 650; }
.stats-heading h2 { margin: 7px 0 8px; font-size: 21px; }
.stats-heading p, .trend-heading p, .chart-note, .stats-footnote { color: var(--text-secondary); font-size: 12px; line-height: 1.7; }
.stats-heading p { margin: 0; }
.range-options { display: flex; border: 1px solid var(--border-color); border-radius: 8px; padding: 3px; gap: 2px; }
button { font: inherit; font-size: 12px; cursor: pointer; white-space: nowrap; }
.range-options button { background: transparent; border: 0; color: var(--text-secondary); padding: 7px 12px; border-radius: 5px; }
.range-options button[aria-pressed="true"] { background: #e8f0ea; color: #305c43; font-weight: 650; }
.stats-refresh { border: 1px solid var(--border-color); background: transparent; color: var(--text-primary); padding: 9px 12px; border-radius: 7px; }
button:focus-visible, summary:focus-visible { outline: 2px solid #357950; outline-offset: 3px; }
button:disabled { opacity: .6; cursor: wait; }
.stats-health, .stats-notice { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin: 22px 0; padding: 11px 14px; background: #f2f7f2; border-radius: 8px; color: #456653; font-size: 12px; line-height: 1.7; }
.is-warning { background: #fbf5e9; color: #896522; }
.health-dot { width: 6px; height: 6px; background: currentColor; border-radius: 50%; }
.stats-cards { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--border-color); border-radius: 12px; }
.metric { display: flex; flex-direction: column; padding: 22px; gap: 9px; border-right: 1px solid var(--border-color); }
.metric:last-child { border-right: 0; }
.metric > span { font-size: 13px; color: var(--text-secondary); }
.metric strong { font-size: clamp(23px, 3vw, 32px); font-weight: 600; letter-spacing: -.8px; overflow-wrap: anywhere; }
.metric em { font-size: 10px; font-style: normal; padding: 2px 4px; border-radius: 3px; background: #eef3ef; }
.metric small { font-size: 11px; color: var(--text-secondary); }
.trend-heading { margin: 28px 0 6px; }
.trend-heading h3 { font-size: 15px; margin: 0; }
.trend-heading p { margin: 7px 0; }
.trend-heading p strong { color: var(--text-primary); padding-left: 6px; }
.chart-legend { flex-wrap: wrap; justify-content: flex-end; font-size: 11px; color: var(--text-secondary); }
.chart-legend span::before { content: ''; display: inline-block; vertical-align: middle; width: 13px; height: 3px; margin-right: 6px; background: #397956; }
.chart-legend .uv-key::before { background: #6685ae; }
.chart-legend .gap-key::before { height: 10px; width: 6px; background: #e7e3db; }
.chart-scroll { overflow-x: auto; }
.stats-chart { display: block; width: 100%; min-width: 480px; }
.stats-chart text { font-size: 11px; fill: #747e76; }
.grid-line { stroke: #e7ece7; stroke-dasharray: 3 4; }
.gap-band { fill: #e7e3db; opacity: .55; }
.pv-area { fill: #397956; opacity: .08; }
.pv-line, .uv-line { fill: none; stroke-width: 2; stroke-linejoin: round; }
.pv-line { stroke: #397956; }
.uv-line { stroke: #6685ae; stroke-dasharray: 5 3; }
.pv-point { fill: #397956; }
.uv-point { fill: #6685ae; }
.chart-note { margin: 4px 0 18px; }
.daily-details { font-size: 12px; border-top: 1px solid var(--border-color); padding-top: 16px; }
.daily-details summary { cursor: pointer; color: #42664f; width: fit-content; }
.daily-table-wrap { max-height: 330px; overflow: auto; margin-top: 12px; }
table { border-collapse: collapse; width: 100%; text-align: left; min-width: 450px; }
th, td { padding: 10px 12px; border-bottom: 1px solid var(--border-color); }
th { color: var(--text-secondary); background: #f6f8f5; font-weight: 500; }
.stats-footnote { margin-top: 18px; padding-top: 12px; border-top: 1px solid var(--border-color); font-size: 11px; }
.stats-footnote p { margin: 5px 0; }
.stats-footnote span { margin-left: 20px; }
@media (max-width: 700px) {
  .stats-heading, .trend-heading { align-items: flex-start; flex-direction: column; }
  .stats-controls { width: 100%; }
  .stats-cards { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .metric { padding: 18px; }
  .metric:nth-child(2) { border-right: 0; }
  .metric:nth-child(-n+2) { border-bottom: 1px solid var(--border-color); }
  .chart-legend { justify-content: flex-start; }
  .stats-footnote span { display: block; margin-left: 0; }
}
</style>
