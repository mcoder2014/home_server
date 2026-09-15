const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const test = require('node:test')
const vm = require('node:vm')

function statsView(data, days = 7, now = '2026-09-14T18:30:00Z') {
    const filename = path.join(__dirname, '../src/utils/web_project_stats.cjs')
    assert.equal(fs.existsSync(filename), true, '统计应保留每日质量与缺口')
    return require(filename).buildStatsView(data, days, new Date(now))
}

function snapshot(overrides = {}) {
    return {enabled: true, project_id: '123', timezone: 'Asia/Shanghai', uv_method: 'browser_hll',
        tracking_started_at: '2026-09-14T10:00:00+08:00', persisted_at: '2026-09-15T02:29:00+08:00',
        total: {pv: 100, uv: 4}, quality: 'ok', quality_reason: '', daily: [
            {date: '2026-09-14', pv: 10, uv: 3, quality: 'ok'},
            {date: '2026-09-15', pv: 20, uv: 3, quality: 'ok'},
        ], ...overrides}
}

function component(api = {}, projectId = '123') {
    const filename = path.join(__dirname, '../src/components/WebProjectStats.vue')
    assert.equal(fs.existsSync(filename), true, '编辑页应有独立统计面板')
    const source = fs.readFileSync(filename, 'utf8')
    const script = source.match(/<script>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '').replace('export default', 'module.exports =')
    const box = {module: {exports: {}}, console,
        require: name => name === '@/api/web_projects.cjs' ? {webShareApi: api} : require(path.join(__dirname, '../src', name.slice(2))),
    }
    vm.runInNewContext(script, box, {filename})
    const definition = box.module.exports
    const view = {projectId}
    Object.assign(view, definition.data.call(view))
    for (const [key, method] of Object.entries(definition.methods || {})) view[key] = method.bind(view)
    for (const [key, getter] of Object.entries(definition.computed || {})) Object.defineProperty(view, key, {get: () => getter.call(view)})
    return {view, definition}
}

test('statistics use Shanghai today and all-time UV instead of adding daily UV', () => {
    const view = statsView(snapshot())
    assert.equal(view.daily.length, 7)
    assert.equal(view.today.date, '2026-09-15')
    assert.equal(view.today.pv, 20)
    assert.equal(view.total.uv, 4)
    assert.equal(view.periodPV, 30)
    assert.equal(view.daily[0].quality, 'not_started')
    assert.equal(view.daily[0].pv, null)
    assert.equal(Object.hasOwn(view, 'periodUV'), false)
})

test('unknown and degraded dates split trend paths and are never converted to healthy zero', () => {
    const view = statsView(snapshot({tracking_started_at: '2026-09-01T00:00:00+08:00', quality: 'degraded', daily: [
        {date: '2026-09-10', pv: 9, uv: 4, quality: 'ok'},
        {date: '2026-09-11', pv: 0, uv: 0, quality: 'unknown'},
        {date: '2026-09-12', pv: 8, uv: 2, quality: 'ok'},
        {date: '2026-09-13', pv: 5, uv: 1, quality: 'degraded', quality_reason: '采集曾中断'},
        {date: '2026-09-14', pv: 7, uv: 2, quality: 'ok'},
    ]}))
    assert.equal(view.daily.find(day => day.date === '2026-09-11').pv, null)
    assert.equal(view.today.pv, null, '未提供的日期不得伪装为零流量')
    assert.equal(view.paths.pv.length, 3)
    assert.equal(view.periodPV, 29)
    assert.equal(view.periodPartial, true)
    assert.equal(view.daily.find(day => day.date === '2026-09-13').quality_reason, '采集曾中断')
})

test('disabled and not-started statistics do not present current zero traffic', () => {
    const disabled = statsView(snapshot({enabled: false, quality: 'disabled', daily: []}))
    assert.equal(disabled.quality, 'disabled')
    assert.equal(disabled.today.pv, null)
    assert.equal(disabled.total.pv, 100, '停用时保留已持久化的历史累计')
    const fresh = statsView(snapshot({tracking_started_at: null, persisted_at: null, total: {pv: 0, uv: 0}, quality: 'not_started', daily: []}))
    assert.equal(fresh.total.pv, null)
    assert.equal(fresh.periodPV, null)
    assert.equal(fresh.today.quality, 'not_started')
})

test('late day-range response cannot replace the latest selected statistics', async () => {
    const pending = []
    const {view} = component({getStats: (id, days) => new Promise(resolve => pending.push({id, days, resolve}))})
    const oldRequest = view.loadStats()
    view.days = 7
    const newRequest = view.loadStats()
    pending[1].resolve(snapshot({total: {pv: 7, uv: 2}}))
    await newRequest
    pending[0].resolve(snapshot({total: {pv: 30, uv: 5}}))
    await oldRequest
    assert.equal(view.stats.total.pv, 7)
    assert.equal(view.loading, false)
})

test('changing project or unmounting discards pending data and errors', async () => {
    let resolve
    const {view, definition} = component({getStats: () => new Promise(done => {resolve = done})})
    const pending = view.loadStats()
    view.projectId = '456'
    resolve(snapshot())
    await pending
    assert.equal(view.stats, null)
    let reject
    view.projectId = '123'
    const other = component({getStats: () => new Promise((done, fail) => {reject = fail})})
    const request = other.view.loadStats()
    definition.beforeUnmount.call(other.view)
    reject(new Error('stale failure'))
    await request
    assert.equal(other.view.error, '')
})

test('forbidden statistics clear prior counts without failing the editor', async () => {
    const {view} = component({getStats: async () => {throw Object.assign(new Error('forbidden'), {status: 403})}})
    view.stats = snapshot()
    await assert.doesNotReject(() => view.loadStats())
    assert.equal(view.stats, null)
    assert.match(view.error, /无权|所有者/)
    assert.equal(view.loading, false)
})

test('a response for another project is not rendered even if it returns successfully', async () => {
    const {view} = component({getStats: async () => snapshot({project_id: '456'})})
    await view.loadStats()
    assert.equal(view.stats, null)
    assert.match(view.error, /不匹配/)
})
