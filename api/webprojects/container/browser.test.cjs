// 本地浏览器验收：真实 Chromium 与 DOM；HTTP 接口使用本地 fixture，不连接生产。
const {test, before, after} = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const http = require('node:http')
let playwrightModule = process.env.PLAYWRIGHT_PATH
if (!playwrightModule) {
  try { playwrightModule = require.resolve('playwright') }
  catch (_) { playwrightModule = path.join(require('node:os').homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright') }
}
const {chromium} = require(playwrightModule)
let browser, server, origin
const source = path.join(__dirname, 'runtime.js')
const context = {user_id: '42', owner_user_id: '42', display_name: '测试用户', can_comment: true, csrf_token: 'test-csrf', release_id: '11', entry_file: 'index.html', container_mode: 'enhanced'}
const html = `<!doctype html><html lang="zh-CN" data-hs-comment-schema="1" data-hs-page-id="trip-guide"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>真实交互测试页</title><style>body{margin:90px 30px;font:18px sans-serif}#map{height:200px;background:#edf4e9}#business{padding:20px}p{max-width:700px}</style><body><main data-hs-comment-root><p id="paragraph" data-hs-comment-id="intro" data-hs-comment-kind="text">上午参观博物馆，下午沿河步行。</p><p id="legacy">没有结构标记也能选择唯一文本。</p><div data-hs-comment-ignore id="secret">私人表单内容<input value="secret"></div><figure data-hs-comment-id="photo" data-hs-comment-kind="image" data-hs-comment-label="路线照片"><img alt="路线照片" src="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='100' height='40'%3E%3Crect fill='green' width='100' height='40'/%3E%3C/svg%3E"></figure><figure id="map" data-hs-comment-id="route-map" data-hs-comment-kind="module" data-hs-comment-interaction="preserve" data-hs-comment-label="路线地图"><button id="business">打开地图信息窗</button><div id="map-state">地图内部文本不能被读取为评论引用</div></figure></main><script>window.businessClicks=0;document.querySelector('#business').addEventListener('click',()=>window.businessClicks++);window.mapBefore=document.querySelector('#map').innerHTML;</script><script src="/api/web-share/container.js" data-project-id="7" data-release-id="11" data-entry-file="index.html"></script></body></html>`

before(async () => {
  server = http.createServer((req, res) => {
    if (req.url === '/api/web-share/container.js') {
      res.setHeader('Content-Type', 'application/javascript')
      res.end(fs.existsSync(source) ? fs.readFileSync(source) : '')
    } else if (req.url.includes('/deleted.html')) {
      res.statusCode = 404
      res.end('not found')
    } else { res.setHeader('Content-Type', 'text/html; charset=utf-8'); res.end(req.url.includes('defer=1') ? html.replace('<script src="/api/web-share/container.js"', '<script defer src="/api/web-share/container.js"') : html) }
  })
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
  origin = `http://127.0.0.1:${server.address().port}`
  const installedChrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
  const executablePath = process.env.CHROME_PATH || (process.platform === 'darwin' && fs.existsSync(installedChrome) ? installedChrome : undefined)
  browser = await chromium.launch({headless: true, ...(executablePath ? {executablePath} : {})})
})
after(async () => { if (browser) await browser.close(); if (server) await new Promise(resolve => server.close(resolve)) })

function thread(id, overrides = {}) {
  return {id, project_id: '7', page_key: 'id:trip-guide', page_path: 'index.html', release_id: '10', anchor: {kind: 'text', target_id: 'intro', exact: '参观博物馆', page_id: 'trip-guide'}, status: 'open', revision: 1, author_user_id: '42', author_application_id: '8', created_at: '2026-09-16T00:00:00Z', ...overrides}
}
async function setup(options = {}) {
  const page = await browser.newPage({viewport: {width: options.width || (options.mobile ? 390 : 1280), height: 850}})
  if (options.moduleGlobal) await page.addInitScript(() => { window.module = {exports: {business: true}} })
  page.setDefaultTimeout(10000)
  const state = {identity: {...context, ...options.identity}, requests: [], writes: [], threads: options.threads || [], events: [{id: '1', sequence: 1, kind: 'comment', body: '原始讨论', actor_user_id: '42', actor_application_id: '8', created_at: '2026-09-16T00:00:00Z'}, {id: '2', sequence: 2, kind: 'reply', body: '历史回复', actor_user_id: '43', actor_application_id: '', created_at: '2026-09-16T00:01:00Z'}]}
  await page.route('**/api/web-share/7/**', async route => {
    const request = route.request(), url = new URL(request.url())
    state.requests.push(url.pathname + url.search)
    let data
    if (url.pathname.endsWith('/view-context')) data = state.identity
    else if (request.method() === 'POST') {
      state.writes.push({path: url.pathname, body: request.postDataJSON(), headers: request.headers()})
      if (options.failWrite) { await route.abort('failed'); return }
      data = thread('new', {anchor: request.postDataJSON().anchor || thread('1').anchor})
    } else if (url.pathname.endsWith('/events')) {
      data = url.searchParams.get('cursor') ? {items: state.events.slice(1), has_more: false} : {items: state.events.slice(0, 1), has_more: true, next_cursor: '1'}
    } else if (url.pathname.endsWith('/comment-threads')) {
      if (options.delay) await new Promise(resolve => setTimeout(resolve, options.delay))
      data = url.searchParams.get('cursor') ? {items: state.threads.slice(1), has_more: false} : {items: state.threads.slice(0, 1), has_more: state.threads.length > 1, next_cursor: 'next'}
    } else data = state.threads[0]
    await route.fulfill({json: {code: 0, data}}).catch(() => {})
  })
  await page.goto(origin + '/p/trip/' + (options.defer ? '?defer=1' : ''))
  return {page, state}
}
async function selectText(page, id, start = 0, end) {
  await page.evaluate(({id, start, end}) => {
    const text = document.getElementById(id).firstChild, range = document.createRange()
    range.setStart(text, start); range.setEnd(text, end == null ? text.length : end)
    const selection = window.getSelection(); selection.removeAllRanges(); selection.addRange(range)
    document.dispatchEvent(new Event('selectionchange'))
  }, {id, start, end})
}

test('匿名只有固定菜单且原网页按钮可用，无评论请求或可聚焦入口', async () => {
  const {page, state} = await setup({identity: {user_id: '', display_name: '', can_comment: false, csrf_token: ''}})
  try {
    await page.getByRole('link', {name: '系统主页'}).waitFor()
    assert.ok((await page.getByRole('navigation', {name: '网页工具'}).evaluate(node => getComputedStyle(node).fontFamily)).includes('system-ui'))
    assert.equal(await page.getByRole('link', {name: '网页托管'}).getAttribute('href'), origin + '/web-share')
    assert.equal(await page.getByRole('button', {name: '评论', exact: true}).count(), 0)
    await page.locator('#business').click()
    assert.equal(await page.evaluate(() => window.businessClicks), 1)
    assert.equal(state.requests.filter(url => url.includes('comment-threads')).length, 0)
    assert.equal(await page.getByRole('complementary', {name: '网页评论'}).count(), 0)
  } finally { await page.close() }
})

test('后端 defer 注入可初始化，菜单展示项目名', async () => {
  const {page} = await setup({defer: true, identity: {project_name: '旅行方案'}})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).waitFor()
    await page.getByText('旅行方案', {exact: true}).waitFor()
  } finally { await page.close() }
})

test('默认浏览，评论模式加载全部分页并展示回复事件和双身份', async () => {
  const {page, state} = await setup({threads: [thread('1'), thread('2', {status: 'resolved'})]})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).waitFor()
    assert.equal(state.requests.filter(url => url.includes('comment-threads')).length, 0)
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByText('讨论 2', {exact: true}).waitFor()
    await page.getByRole('button', {name: '查看讨论 1', exact: true}).click()
    await page.getByText('历史回复', {exact: true}).waitFor()
    if (process.env.CONTAINER_SCREENSHOTS) {
      fs.mkdirSync(process.env.CONTAINER_SCREENSHOTS, {recursive: true})
      await page.screenshot({path: path.join(process.env.CONTAINER_SCREENSHOTS, 'desktop-comments.png'), fullPage: true})
    }
    assert.ok(await page.getByText('用户 42 · 应用 8', {exact: true}).count())
    assert.equal(state.requests.filter(url => url.includes('/events')).length, 2)
    await page.getByRole('button', {name: '标记已解决', exact: true}).click()
    assert.equal(state.writes[0].headers['if-match'], '1')
    assert.equal(state.writes[0].headers['x-csrf-token'], 'test-csrf')
  } finally { await page.close() }
})

test('文本选区创建引用、忽略区拒绝，图片和保护模块从面板选择且不改变原 DOM', async () => {
  const {page, state} = await setup()
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('complementary', {name: '网页评论'}).waitFor()
    await selectText(page, 'secret')
    assert.equal(await page.getByRole('button', {name: '评论所选文字'}).count(), 0)
    await selectText(page, 'paragraph', 2, 7)
    await page.getByRole('button', {name: '评论所选文字'}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('核对开放时间')
    await page.getByRole('button', {name: '提交评论', exact: true}).click()
    assert.equal(state.writes[0].body.anchor.exact, '参观博物馆')
    assert.equal(state.writes[0].body.anchor.target_id, 'intro')
    assert.equal(state.writes[0].body.page_key, 'id:trip-guide')
    await page.getByRole('button', {name: '评论模块：路线地图'}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('这里增加公交路线')
    await page.getByRole('button', {name: '提交评论', exact: true}).click()
    assert.equal(state.writes[1].body.anchor.kind, 'module')
    assert.equal(state.writes[1].body.anchor.target_id, 'route-map')
    await page.locator('#business').click()
    assert.equal(await page.evaluate(() => window.businessClicks), 1)
    assert.equal(await page.evaluate(() => document.querySelector('#map').innerHTML === window.mapBefore), true)
    await page.getByRole('button', {name: '评论图片：路线照片'}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('图片说明')
    await page.getByRole('button', {name: '提交评论', exact: true}).click()
    assert.equal(state.writes[2].body.anchor.kind, 'image')
  } finally { await page.close() }
})

test('失锚和已删除页面的未解决讨论显示在页尾，仍存在的其他页面不误报', async () => {
  const {page} = await setup({threads: [
    thread('1', {anchor: {kind: 'text', target_id: 'intro', exact: '已删除原文'}}),
    thread('2', {page_key: 'path:deleted.html', page_path: 'deleted.html'}),
    thread('3', {page_key: 'id:other-page', page_path: 'other.html'}),
    thread('4', {page_key: 'id:renamed-page', page_path: 'deleted.html', anchor: {kind: 'page', label: '已改名页面'}}),
  ]})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('region', {name: '无法定位的未解决讨论'}).waitFor()
    await page.waitForTimeout(250)
    const footer = page.getByRole('region', {name: '无法定位的未解决讨论'})
    assert.ok(await page.getByText('原文无法定位', {exact: false}).count())
    assert.ok(await footer.getByText('页面已删除：deleted.html', {exact: true}).count())
    assert.equal(await footer.getByText('other.html', {exact: false}).count(), 0)
    assert.ok(await footer.getByText('原页面路径不可访问，可能已改名或删除：deleted.html', {exact: true}).count())
    assert.ok(await footer.getByText('已改名页面', {exact: true}).count())
    assert.equal(await page.locator('[data-hs-container-marker]').count(), 0)
    const churn = await page.evaluate(async () => {
      let count = 0
      const observer = new MutationObserver(records => { count += records.length })
      observer.observe(document.body, {childList: true})
      await new Promise(resolve => setTimeout(resolve, 120))
      observer.disconnect()
      return count
    })
    assert.equal(churn, 0, '失锚页尾不应触发容器自身的无限重绘')
  } finally { await page.close() }
})

test('选区被收起后不遗留文字入口，普通未标记文字仍能唯一定位', async () => {
  const {page, state} = await setup()
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('complementary', {name: '网页评论'}).waitFor()
    await selectText(page, 'legacy')
    await page.getByRole('button', {name: '评论所选文字'}).waitFor()
    await page.evaluate(() => { window.getSelection().removeAllRanges(); document.dispatchEvent(new Event('selectionchange')) })
    assert.equal(await page.getByRole('button', {name: '评论所选文字'}).count(), 0)
    await selectText(page, 'legacy')
    await page.getByRole('button', {name: '评论所选文字'}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('未标记正文')
    await page.getByRole('button', {name: '提交评论', exact: true}).click()
    assert.equal(state.writes[0].body.anchor.target_id, '')
    assert.equal(state.writes[0].body.anchor.exact, '没有结构标记也能选择唯一文本。')
  } finally { await page.close() }
})

test('返回浏览和身份切换清理选区、草稿、标记与面板，迟到响应无法恢复评论', async () => {
  const {page, state} = await setup({threads: [thread('1')], delay: 500})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('complementary', {name: '网页评论'}).waitFor()
    await selectText(page, 'paragraph', 2, 7)
    await page.getByRole('button', {name: '评论所选文字'}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('旧账号草稿')
    await page.getByRole('button', {name: '返回浏览'}).click()
    await page.waitForTimeout(650)
    assert.equal(await page.getByRole('complementary', {name: '网页评论'}).count(), 0)
    assert.equal(await page.getByRole('textbox').count(), 1) // 仅原网页 input。
    assert.equal(await page.locator('[data-hs-container-marker]').count(), 0)
    await selectText(page, 'paragraph', 2, 7)
    assert.equal(await page.getByRole('button', {name: '评论所选文字'}).count(), 0)
    await page.getByRole('button', {name: '评论', exact: true}).click()
    state.identity = {...context, user_id: '43', display_name: '新账号'}
    await page.evaluate(() => window.dispatchEvent(new Event('focus')))
    await page.getByText('新账号', {exact: true}).waitFor()
    assert.equal(await page.getByRole('complementary', {name: '网页评论'}).count(), 0)
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '评论整页'}).click()
    assert.equal(await page.getByRole('textbox', {name: '评论内容'}).inputValue(), '')
    await page.evaluate(() => window.dispatchEvent(new Event('account-session-expired')))
    assert.equal(await page.getByRole('button', {name: '评论', exact: true}).count(), 0)
  } finally { await page.close() }
})

test('hash 路由变化回到浏览并清理旧页面草稿', async () => {
  const {page} = await setup()
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '评论整页'}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('上一页草稿')
    await page.evaluate(() => { location.hash = '#other' })
    await page.getByRole('button', {name: '评论', exact: true}).waitFor()
    assert.equal(await page.getByRole('complementary', {name: '网页评论'}).count(), 0)
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '评论整页'}).click()
    assert.equal(await page.getByRole('textbox', {name: '评论内容'}).inputValue(), '')
  } finally { await page.close() }
})

test('网络写失败不自动重试并提醒核对，窄屏菜单与面板仍可用', async () => {
  const {page, state} = await setup({mobile: true, failWrite: true})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '评论整页'}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('失败测试')
    await page.getByRole('button', {name: '提交评论', exact: true}).click()
    await page.getByText('请求结果未知，请先刷新列表核对，避免重复提交。', {exact: true}).waitFor()
    assert.equal(state.writes.length, 1)
    await page.getByRole('button', {name: '提交评论', exact: true}).click()
    await page.getByText('请求结果未知，请先刷新列表核对，避免重复提交。', {exact: true}).waitFor()
    assert.equal(state.writes.length, 2)
    assert.equal(state.writes[0].body.request_id, state.writes[1].body.request_id, '明确重试必须复用幂等键')
    const box = await page.getByRole('complementary', {name: '网页评论'}).boundingBox()
    assert.ok(box.x >= 0 && box.x + box.width <= 390)
    if (process.env.CONTAINER_SCREENSHOTS) {
      fs.mkdirSync(process.env.CONTAINER_SCREENSHOTS, {recursive: true})
      await page.screenshot({path: path.join(process.env.CONTAINER_SCREENSHOTS, 'mobile-comments.png'), fullPage: true})
    }
  } finally { await page.close() }
})

test('重复目标 ID 和跨 ignore 的选区不能关联，标记未知版本只开放整页评论', async () => {
  const {page} = await setup()
  try {
    await page.evaluate(() => {
      const copy = document.querySelector('#paragraph').cloneNode(true)
      copy.removeAttribute('id'); document.querySelector('main').append(copy)
    })
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('complementary', {name: '网页评论'}).waitFor()
    await selectText(page, 'paragraph', 2, 7)
    assert.equal(await page.getByRole('button', {name: '评论所选文字'}).count(), 0)
    await page.evaluate(() => {
      const range = document.createRange(); range.selectNodeContents(document.querySelector('main'))
      window.getSelection().removeAllRanges(); window.getSelection().addRange(range)
      document.dispatchEvent(new Event('selectionchange'))
    })
    assert.equal(await page.getByRole('button', {name: '评论所选文字'}).count(), 0)
    await page.getByRole('button', {name: '返回浏览'}).click()
    await page.evaluate(() => document.documentElement.dataset.hsCommentSchema = '99')
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByText('页面标记无法校验，可使用整页评论。', {exact: true}).waitFor()
    assert.equal(await page.getByRole('button', {name: '评论模块：路线地图'}).count(), 0)
    await page.getByRole('button', {name: '评论整页'}).waitFor()
  } finally { await page.close() }
})

test('已解决主题可回复与重开，重新关联携带修订号和页面身份', async () => {
  const {page, state} = await setup({threads: [thread('1', {status: 'resolved'})]})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '查看讨论 1', exact: true}).click()
    await page.getByRole('button', {name: '重新打开', exact: true}).waitFor()
    await page.getByRole('textbox', {name: '回复内容'}).fill('已解决后补充说明')
    await page.getByRole('button', {name: '提交回复', exact: true}).click()
    assert.equal(state.writes[0].body.body, '已解决后补充说明')
    assert.equal(state.writes[0].body.release_id, '11')
    await page.getByRole('button', {name: '重新打开', exact: true}).click()
    assert.ok(state.writes[1].path.endsWith('/reopen'))
    assert.equal(state.writes[1].body.release_id, '11')
    await page.getByRole('textbox', {name: '回复内容'}).waitFor()
    await selectText(page, 'legacy')
    await page.getByRole('button', {name: '用所选位置重新关联', exact: true}).click()
    assert.equal(state.writes[2].body.release_id, '11')
    assert.equal(state.writes[2].body.page_key, 'id:trip-guide')
    assert.equal(state.writes[2].body.page_path, 'index.html')
    assert.equal(state.writes[2].headers['if-match'], '1')
  } finally { await page.close() }
})

test('正文长度遵循 API 上限，页面 ID 路由切换清理评论', async () => {
  const {page} = await setup()
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '评论整页'}).click()
    assert.equal(await page.getByRole('textbox', {name: '评论内容'}).getAttribute('maxlength'), '4000')
    await page.evaluate(() => document.documentElement.dataset.hsPageId = 'second-page')
    await page.getByRole('button', {name: '评论', exact: true}).waitFor()
    assert.equal(await page.getByRole('complementary', {name: '网页评论'}).count(), 0)
  } finally { await page.close() }
})

test('保护模块内部伪造标记不参与结构扫描，保护模块不能成为文本评论来源', async () => {
  const {page} = await setup()
  try {
    await page.evaluate(() => {
      document.querySelector('#map-state').setAttribute('data-hs-comment-root', '')
      document.querySelector('#map-state').setAttribute('data-hs-comment-id', 'intro')
      document.querySelector('#map-state').setAttribute('data-hs-comment-kind', 'text')
    })
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '评论模块：路线地图'}).waitFor()
    await selectText(page, 'map-state')
    assert.equal(await page.getByRole('button', {name: '评论所选文字'}).count(), 0)
    await selectText(page, 'paragraph', 2, 7)
    await page.getByRole('button', {name: '评论所选文字'}).waitFor()
  } finally { await page.close() }
})

test('无效选区会清理前一次重新关联锚点', async () => {
  const {page} = await setup({threads: [thread('1')]})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: '查看讨论 1', exact: true}).click()
    await page.getByRole('textbox', {name: '回复内容'}).waitFor()
    await selectText(page, 'legacy')
    await page.getByRole('button', {name: '用所选位置重新关联', exact: true}).waitFor()
    await selectText(page, 'secret')
    assert.equal(await page.getByRole('button', {name: '用所选位置重新关联', exact: true}).count(), 0)
  } finally { await page.close() }
})

test('动态结构失效清除选区入口，保护模块外层标签可更新', async () => {
  const {page} = await setup()
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('complementary', {name: '网页评论'}).waitFor()
    await selectText(page, 'paragraph', 2, 7)
    await page.getByRole('button', {name: '评论所选文字'}).waitFor()
    await page.evaluate(() => document.documentElement.dataset.hsCommentSchema = '99')
    await page.getByText('页面标记无法校验，可使用整页评论。', {exact: true}).waitFor()
    assert.equal(await page.getByRole('button', {name: '评论所选文字'}).count(), 0)
    await page.evaluate(() => document.documentElement.dataset.hsCommentSchema = '1')
    await page.getByRole('button', {name: '评论模块：路线地图'}).waitFor()
    await page.evaluate(() => document.querySelector('#map').dataset.hsCommentLabel = '公交地图')
    await page.getByRole('button', {name: '评论模块：公交地图'}).waitFor()
  } finally { await page.close() }
})

test('超长模块标签按 API 的 Unicode 上限截断', async () => {
  const {page, state} = await setup()
  try {
    const label = '路线'.repeat(150)
    await page.evaluate(value => { document.querySelector('#map').dataset.hsCommentLabel = value }, label)
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByRole('button', {name: /^评论模块：路线/}).click()
    await page.getByRole('textbox', {name: '评论内容'}).fill('标签边界')
    await page.getByRole('button', {name: '提交评论', exact: true}).click()
    assert.equal(Array.from(state.writes[0].body.anchor.label).length, 256)
  } finally { await page.close() }
})

test('320像素窄屏与长名称下菜单不覆盖评论面板', async () => {
  const {page} = await setup({width: 320, identity: {project_name: '长名称旅行路线设计方案', display_name: '非常长的测试用户名称'}})
  try {
    await page.getByRole('button', {name: '评论', exact: true}).click()
    await page.getByText('暂无讨论', {exact: true}).waitFor()
    const menu = await page.getByRole('navigation', {name: '网页工具'}).boundingBox()
    const panel = await page.getByRole('complementary', {name: '网页评论'}).boundingBox()
    assert.ok(panel.y >= menu.y + menu.height, `菜单底部 ${menu.y + menu.height}，面板顶部 ${panel.y}`)
  } finally { await page.close() }
})

test('页面已有 CommonJS 全局对象不会被容器测试导出覆盖', async () => {
  const {page} = await setup({moduleGlobal: true})
  try {
    await page.getByRole('link', {name: '系统主页'}).waitFor()
    assert.deepEqual(await page.evaluate(() => window.module.exports), {business: true})
  } finally { await page.close() }
})

test('作为原页面 iframe 模块加载时不注入第二套菜单', async () => {
  const page = await browser.newPage({viewport: {width: 900, height: 700}})
  try {
    await page.setContent(`<iframe title="业务模块" src="${origin}/p/trip/"></iframe>`)
    const frame = page.frames().find(item => item !== page.mainFrame())
    await frame.locator('#business').waitFor()
    assert.equal(await frame.locator('hs-web-container').count(), 0)
    await frame.locator('#business').click()
    assert.equal(await frame.evaluate(() => window.businessClicks), 1)
  } finally { await page.close() }
})
