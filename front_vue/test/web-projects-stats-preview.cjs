// 本地 UI 预览：复用真实统计组件，所有接口数据均为模拟，不连接后端。
const fs = require('node:fs')
const path = require('node:path')

const output = process.argv[2] || '/private/tmp/home-server-stats-preview'
fs.mkdirSync(output, {recursive: true})
const source = fs.readFileSync(path.join(__dirname, '../src/components/WebProjectStats.vue'), 'utf8')
const template = source.match(/<template>([\s\S]*?)<\/template>\s*<script>/)[1]
const script = source.match(/<script>([\s\S]*?)<\/script>/)[1].replace('export default', 'module.exports =')
const style = source.match(/<style scoped>([\s\S]*?)<\/style>/)[1]
const behavior = fs.readFileSync(path.join(__dirname, '../src/utils/web_project_stats.cjs'), 'utf8')
fs.copyFileSync(require.resolve('vue/dist/vue.global.prod.js'), path.join(output, 'vue.js'))

const html = `<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>访问统计 · 本地模拟预览</title>
<style>:root{--text-primary:#283b31;--text-secondary:#748075;--border-color:#e2e8df}*{box-sizing:border-box}body{margin:0;background:#f5f7f2;color:var(--text-primary);font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}.preview{max-width:1120px;margin:36px auto;padding:0 20px}.preview-header{display:flex;justify-content:space-between;align-items:center;gap:20px;margin-bottom:25px}.preview-header h1{font-size:20px}.preview-header p{font-size:12px;color:var(--text-secondary)}.preview-header select{padding:8px;border:1px solid #cdd7c8;border-radius:7px}.card{background:white;border:1px solid var(--border-color);border-radius:16px;padding:28px}@media(max-width:600px){.preview{padding:0 12px;margin-top:18px}.preview-header{align-items:start;flex-direction:column}.card{padding:18px}}${style}</style>
<div id="app" class="preview"><div class="preview-header"><div><h1>Home Server · 访问统计预览</h1><p>本地模拟数据 · 无生产请求 · 复用实际 Vue 组件</p></div><label>场景 <select v-model="scenario"><option v-for="item in scenarios" :value="item.value">{{item.label}}</option></select></label></div><web-project-stats :key="scenario" project-id="123" /></div>
<script src="./vue.js"></script><script>
const behavior = (() => { const module = {exports: {}}; ${behavior}; return module.exports })();
let selectedScenario = 'ok';
function fixture(days) {
  const date = new Intl.DateTimeFormat('en-CA', {timeZone:'Asia/Shanghai',year:'numeric',month:'2-digit',day:'2-digit'}).format(new Date());
  const end = Date.parse(date + 'T00:00:00Z');
  const dates = Array.from({length: days}, (_, i) => new Date(end - (days - i - 1) * 86400000).toISOString().slice(0,10));
  const quality = selectedScenario;
  return {project_id:'123',enabled:quality !== 'disabled',timezone:'Asia/Shanghai',uv_method:'browser_hll',
    tracking_started_at:quality === 'not_started' ? null : new Date(end - 365 * 86400000).toISOString(),
    persisted_at:quality === 'not_started' ? null : new Date(Date.now()-60000).toISOString(),
    total:quality === 'not_started' ? {pv:0,uv:0} : {pv:84532,uv:8194},quality,quality_reason:'',
    daily:quality === 'not_started' ? [] : dates.map((date,i)=>({date,pv:Math.round(160+95*Math.sin(i*.62)+i*4),uv:Math.round(55+32*Math.sin(i*.53)+i),
      quality: ['degraded','unknown'].includes(quality) && i > days-7 && i < days-3 ? quality : 'ok',
      quality_reason: ['degraded','unknown'].includes(quality) && i > days-7 && i < days-3 ? '采集曾中断，存在缺口' : ''}))};
}
const previewApi = {getStats: async (id,days) => {
  if(selectedScenario === 'forbidden') throw Object.assign(new Error('forbidden'),{status:403});
  await new Promise(resolve=>setTimeout(resolve,180)); return fixture(days);
}};
const component = (()=>{ const module = {exports:{}};const require = name => name.includes('/api/') ? {webShareApi: previewApi} : behavior; ${script};return module.exports;})();
component.template = ${JSON.stringify(template)};
Vue.createApp({components:{WebProjectStats:component},data(){return{scenario:'ok',scenarios:[{value:'ok',label:'正常采集'},{value:'not_started',label:'尚未开始'},{value:'disabled',label:'统计关闭'},{value:'degraded',label:'采集不完整'},{value:'unknown',label:'未知缺口'},{value:'forbidden',label:'权限拒绝'}]}},watch:{scenario(value){selectedScenario=value}}}).mount('#app');
</script></html>`
fs.writeFileSync(path.join(output, 'index.html'), html)
console.log(`本地模拟预览已生成：${path.join(output, 'index.html')}`)
