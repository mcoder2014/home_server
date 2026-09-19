// 用真实 Vue / Element Plus / Chromium 验证说明书 SFC；网络仅访问本地合成 fixture。
const {test, before, after} = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs'), path = require('node:path'), http = require('node:http')
const {parse, compileTemplate, compileStyle} = require('@vue/compiler-sfc')
let playwrightPath = process.env.PLAYWRIGHT_PATH
if (!playwrightPath) {
    try { playwrightPath = require.resolve('playwright') }
    catch (_) { playwrightPath = path.join(require('node:os').homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright') }
}
const {chromium} = require(playwrightPath)
const root = path.join(__dirname, '../src')
let browser, server, origin

function fixturePDF() {
    const stream = 'BT /F1 24 Tf 28 100 Td (Manual preview) Tj ET\n'
    const objects = [
        '1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n',
        '2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n',
        '3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 240 180] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>\nendobj\n',
        `4 0 obj\n<< /Length ${Buffer.byteLength(stream)} >>\nstream\n${stream}endstream\nendobj\n`,
        '5 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n',
    ]
    let body = '%PDF-1.4\n'
    const offsets = [0]
    for (const object of objects) { offsets.push(Buffer.byteLength(body)); body += object }
    const xref = Buffer.byteLength(body)
    body += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`
    for (const offset of offsets.slice(1)) body += `${String(offset).padStart(10, '0')} 00000 n \n`
    body += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xref}\n%%EOF\n`
    return Buffer.from(body)
}

const artifacts = {
    '/vue.js': fs.readFileSync(require.resolve('vue/dist/vue.global.prod.js')),
    '/element.js': fs.readFileSync(require.resolve('element-plus/dist/index.full.js')),
    '/element.css': fs.readFileSync(require.resolve('element-plus/dist/index.css')),
    '/axios.js': fs.readFileSync(require.resolve('axios/dist/axios.js')),
}
let script = 'const __modules={axios:window.axios};\n'
for (const name of ['api/browser_client.cjs', 'utils/manuals_behavior.cjs', 'api/manuals.cjs']) {
    const key = name === 'api/browser_client.cjs' ? './browser_client.cjs' : name === 'api/manuals.cjs' ? '@/api/manuals.cjs' : '@/utils/manuals_behavior.cjs'
    script += `(function(){const module={exports:{}};const require=name=>__modules[name];\n${fs.readFileSync(path.join(root, name), 'utf8')}\n__modules[${JSON.stringify(key)}]=module.exports;})();\n`
}
script += "__modules['@/api/browser_client.cjs']=__modules['./browser_client.cjs'];\n"
script += "const MyHeader={template:'<header class=\"fixture-header\"><a href=\"/\">CQ</a><nav><a href=\"/manuals\">说明书</a></nav></header>'};\n"
let css = fs.readFileSync(path.join(root, 'assets/global.css'), 'utf8')
for (const name of ['ManualList', 'ManualEditor', 'ManualDetail']) {
    const {descriptor} = parse(fs.readFileSync(path.join(root, 'views', name + '.vue'), 'utf8'))
    const scope = 'data-v-manual-' + name.toLowerCase()
    const compiled = compileTemplate({source: descriptor.template.content, id: scope, compilerOptions: {mode: 'function'}})
    assert.deepEqual(compiled.errors, [])
    const component = descriptor.script.content.replace(/^import .*$/gm, '').replace('export default', 'const definition =')
    script += `const ${name}=(function(){const require=name=>__modules[name];${component};definition.render=new Function('Vue',${JSON.stringify(compiled.code)})(Vue);definition.__scopeId=${JSON.stringify(scope)};return definition;})();\n`
    for (const style of descriptor.styles) css += compileStyle({source: style.content, id: scope, scoped: style.scoped}).code
}
script += `
const params=new URLSearchParams(location.search);
const user=params.get('as')==='owner'?{id:'42',status:'active',user_name:'owner',csrf_token:'fixture-csrf',capabilities:{manuals:true}}:null;
const state=Vue.reactive({userInfo:user,site:{title:'合成家庭服务'},modules:{manuals:true},sessionLoaded:true});
const store={state,commit(name,value){if(name==='REMOVE_INFO')state.userInfo=null;else if(name==='SET_USERINFO')state.userInfo=value;},dispatch(){return Promise.resolve();}};
__modules['@/api/browser_client.cjs'].configureBrowserSession(()=>state.userInfo?.csrf_token||'',()=>store.commit('REMOVE_INFO'));
const segments=location.pathname.split('/').filter(Boolean),route=Vue.reactive({path:location.pathname,fullPath:location.pathname+location.search,params:{id:segments[1]==='new'?'':segments[1]||''},query:{}});
const navigations=[];const router={push(value){navigations.push(value);route.path=typeof value==='string'?value:value.path;},replace(value){navigations.push(value);route.path=typeof value==='string'?value:value.path;}};
const RouterLink={props:['to'],setup(props,{slots}){return()=>Vue.h('a',{href:typeof props.to==='string'?props.to:props.to.path},slots.default?slots.default():[])}};
let page=ManualList;if(location.pathname==='/manuals/new'||location.pathname.endsWith('/edit'))page=ManualEditor;else if(segments.length===2)page=ManualDetail;
const app=Vue.createApp({render(){return Vue.h(page,{ref:'page'});},mounted(){window.manualPage=this.$refs.page;}});
app.use(ElementPlus);app.component('RouterLink',RouterLink);Object.assign(app.config.globalProperties,{$store:store,$route:route,$router:router,$message:ElementPlus.ElMessage,$confirm:ElementPlus.ElMessageBox.confirm});app.mount('#app');window.fixtureStore=store;window.fixtureRoute=route;window.fixtureNavigations=navigations;
`
artifacts['/manuals.js'] = Buffer.from(script)
artifacts['/manuals.css'] = Buffer.from(css)
artifacts['/content/front.png'] = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')
artifacts['/thumb/saved-image.png'] = artifacts['/content/front.png']
artifacts['/thumb/saved-pdf.png'] = artifacts['/content/front.png']
artifacts['/content/guide.pdf'] = fixturePDF()
const html = '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/element.css"><link rel="stylesheet" href="/manuals.css"><body><div id="app"></div><script src="/vue.js"></script><script src="/element.js"></script><script src="/axios.js"></script><script src="/manuals.js"></script></body></html>'

before(async () => {
    server = http.createServer((req, res) => {
        const artifact = artifacts[req.url]
        const contentType = req.url.endsWith('.css') ? 'text/css' : req.url.endsWith('.png') ? 'image/png' : req.url.endsWith('.pdf') ? 'application/pdf' : 'application/javascript'
        res.setHeader('Content-Type', artifact ? contentType : 'text/html; charset=utf-8')
        if (req.url.endsWith('.pdf')) {
            res.setHeader('Content-Disposition', 'inline; filename="manual.pdf"')
            res.setHeader('Content-Security-Policy', 'sandbox')
        }
        res.end(artifact || html)
    })
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
    origin = 'http://127.0.0.1:' + server.address().port
    const chrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
    const executablePath = process.env.CHROME_PATH || (process.platform === 'darwin' && fs.existsSync(chrome) ? chrome : undefined)
    browser = await chromium.launch({headless: true, ...(executablePath ? {executablePath} : {})})
})
after(async () => { if (browser) await browser.close(); if (server) await new Promise(resolve => server.close(resolve)) })

function baseManual(overrides = {}) {
    return {
        id: '200', name: '咖啡机', description: '日常保养', categories: ['厨房', '家电'], access_mode: 'public', status: 'active',
        revision: 4, cover_item_id: null, item_count: 1, can_edit: false, update_time: '2026-09-19T03:00:00Z', items: [],
        cover: {id: '501', kind: 'text', title: '快速入门', text_excerpt: '先清洗滤网，再加入咖啡豆。', thumbnail_url: '', url_host: '', preview_status: 'none'},
        ...overrides,
    }
}

async function setup(options = {}) {
    const state = options.state || {requests: [], writes: [], uploads: [], uploadAttempts: new Map(), revision: 1}
    const context = await browser.newContext({viewport: {width: options.width || 1280, height: 950}})
    const page = await context.newPage(); page.setDefaultTimeout(10000)
    const errors = []; page.on('pageerror', error => errors.push(error.message))
    await page.route('**/api/manuals**', async route => {
        const request = route.request(), url = new URL(request.url())
        state.requests.push({method: request.method(), url: url.pathname + url.search})
        if (options.handler) {
            const handled = await options.handler({route, request, url, state})
            if (handled) return
        }
        if (url.pathname === '/api/manuals/categories') return route.fulfill({json: {code: 0, data: {items: ['厨房']}}})
        if (url.pathname === '/api/manuals') {
            const filtered = url.searchParams.get('q') === '咖啡' && url.searchParams.get('category') === '厨房'
            const items = filtered ? [baseManual()] : [baseManual(), baseManual({id: '201', name: '无分类手册', categories: [], cover: {id: '502', kind: 'url', title: '官网', url_host: 'example.com', text_excerpt: '', thumbnail_url: '', preview_status: 'none'}})]
            return route.fulfill({json: {code: 0, data: {items, has_more: false, next_cursor: ''}}})
        }
        return route.fulfill({status: 404, json: {code: 404, message: 'fixture not found'}})
    })
    await page.goto(origin + (options.path || '/manuals'))
    await page.waitForFunction(() => !!window.manualPage).catch(() => { throw new Error(`manual page did not mount; page errors: ${errors.join('; ')}`) })
    assert.deepEqual(errors, [])
    return {page, context, state, errors}
}

test('匿名列表展示封面摘要，分类与名称组合查询，并显式发送未分类空值', async () => {
    const {page, context, state} = await setup({width: 320})
    try {
        await page.getByText('先清洗滤网，再加入咖啡豆。', {exact: true}).waitFor()
        assert.equal(await page.getByText('预览不可用，原件仍可查看', {exact: true}).count(), 0)
        await page.getByText(/更新于 2026\/9\/19/).first().waitFor()
        assert.equal(await page.getByLabel('只看我的说明书').count(), 0)
        await page.getByLabel('按名称搜索').fill('咖啡')
        await page.getByLabel('分类筛选').selectOption({label: '厨房'})
        await page.getByRole('button', {name: '搜索', exact: true}).click()
        await page.waitForFunction(() => window.manualPage.loading === false)
        assert.match(state.requests.at(-2).url, /q=%E5%92%96%E5%95%A1/)
        assert.match(state.requests.at(-2).url, /category=%E5%8E%A8%E6%88%BF/)
        await page.getByLabel('分类筛选').selectOption({label: '未分类'})
        await page.getByRole('button', {name: '搜索', exact: true}).click()
        await page.waitForFunction(() => window.manualPage.loading === false)
        const requestURL = new URL(state.requests.at(-2).url, 'https://fixture.invalid')
        assert.equal(requestURL.searchParams.has('category'), true)
        assert.equal(requestURL.searchParams.get('category'), '')
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    } finally { await context.close() }
})

test('匿名访问新建和编辑路由会回到登录页', async () => {
    for (const target of ['/manuals/new', '/manuals/200/edit']) {
        const {page, context} = await setup({path: target, width: 320})
        try {
            await page.waitForFunction(() => window.fixtureRoute.path === '/login')
            const navigation = await page.evaluate(() => window.fixtureNavigations.at(-1))
            assert.equal(navigation.path, '/login')
            assert.equal(navigation.query.redirect, target)
        } finally { await context.close() }
    }
})

test('多次选图按稳定队列追加，部分失败仅重试失败项并使用原幂等 key', async () => {
    const state = {requests: [], writes: [], uploads: [], uploadAttempts: new Map(), revision: 1, itemSequence: 0}
    const handler = async ({route, request, url}) => {
        if (url.pathname === '/api/manuals/categories') { await route.fulfill({json: {code: 0, data: {items: ['厨房']}}}); return true }
        if (url.pathname === '/api/manuals' && request.method() === 'POST') {
            const body = request.postDataJSON(); state.writes.push({kind: 'create', body})
            assert.ok(Array.isArray(body.categories)); assert.equal('category' in body, false)
            await route.fulfill({json: {code: 0, data: baseManual({id: '100', name: body.name, categories: body.categories, access_mode: body.access_mode, status: 'draft', revision: 1, can_edit: true, cover: null, items: [], item_count: 0})}}); return true
        }
        if (url.pathname === '/api/manuals/100/files') {
            const body = request.postDataBuffer().toString('latin1')
            const field = name => body.match(new RegExp(`name="${name}"\\r\\n\\r\\n([^\\r]+)`))?.[1] || ''
            const filename = body.match(/filename="([^"]+)"/)?.[1] || ''
            const requestID = field('client_request_id'), attempt = (state.uploadAttempts.get(filename) || 0) + 1
            state.uploadAttempts.set(filename, attempt); state.uploads.push({filename, requestID, attempt})
            if (filename === 'two.png' && attempt === 1) { await route.fulfill({status: 500, json: {code: 50001, message: '合成单项失败'}}); return true }
            state.revision++; state.itemSequence++
            await route.fulfill({json: {code: 0, data: {item: {id: `item-${filename}`, kind: 'image', title: filename, original_name: filename, position: state.itemSequence, content_url: `/content/${filename}`, thumbnail_url: `/thumb/${filename}`, preview_status: 'ready'}, revision: state.revision}}}); return true
        }
        if (url.pathname === '/api/manuals/100' && request.method() === 'PATCH') {
            const body = request.postDataJSON(); state.writes.push({kind: 'patch', body})
            state.revision++
            await route.fulfill({json: {code: 0, data: baseManual({id: '100', status: body.status || 'draft', revision: state.revision, can_edit: true, item_count: body.item_ids.length})}}); return true
        }
        return false
    }
    const {page, context} = await setup({path: '/manuals/new?as=owner', width: 320, state, handler})
    try {
        await page.getByLabel('说明书名称').fill('冰箱')
        const categories = page.locator('.category-field .el-select__input')
        await categories.fill('厨房'); await categories.press('Enter')
        await categories.fill('家电'); await categories.press('Enter')
        const input = page.getByLabel('选择图片')
        const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')
        await input.setInputFiles([{name: 'one.png', mimeType: 'image/png', buffer: png}, {name: 'two.png', mimeType: 'image/png', buffer: png}])
        await input.setInputFiles({name: 'three.png', mimeType: 'image/png', buffer: png})
        assert.equal(await page.locator('.upload-queue-item').count(), 3)
        await page.getByRole('button', {name: '创建并发布', exact: true}).click()
        await page.getByText('合成单项失败', {exact: true}).waitFor()
        assert.equal(state.writes.filter(item => item.kind === 'patch').length, 0)
        assert.deepEqual(await page.locator('.queue-status').allTextContents(), ['已成功', '上传失败', '已成功'])
        const failedID = state.uploads.find(item => item.filename === 'two.png').requestID
        await page.getByRole('button', {name: '重试失败项', exact: true}).click()
        await page.getByText('说明书已发布', {exact: true}).waitFor()
        const retried = state.uploads.filter(item => item.filename === 'two.png')
        assert.equal(retried.length, 2)
        assert.equal(retried[1].requestID, failedID)
        const activation = state.writes.find(item => item.kind === 'patch').body
        assert.deepEqual(activation.item_ids, ['item-one.png', 'item-two.png', 'item-three.png'])
        assert.equal(activation.revision, 4)
        assert.equal(typeof activation.revision, 'number')
        assert.equal('cover_item_id' in activation, false)
        assert.equal(activation.status, 'active')
        assert.deepEqual(state.writes.find(item => item.kind === 'create').body.categories, ['厨房', '家电'])
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    } finally { await context.close() }
})

test('慢上传期间拒绝追加、排序、移除和并发写操作，全部队列成功后才保存', async () => {
    const savedItem = {id: 'saved-1', kind: 'text', title: '既有资料', text: '已保存', position: 1}
    const state = {requests: [], writes: [], uploads: [], revision: 7, itemSequence: 0}
    let markUploadStarted, releaseFirstUpload
    const uploadStarted = new Promise(resolve => { markUploadStarted = resolve })
    const firstUploadReleased = new Promise(resolve => { releaseFirstUpload = resolve })
    const handler = async ({route, request, url}) => {
        if (url.pathname === '/api/manuals/categories') { await route.fulfill({json: {code: 0, data: {items: ['厨房']}}}); return true }
        if (url.pathname === '/api/manuals/400' && request.method() === 'GET') {
            await route.fulfill({json: {code: 0, data: baseManual({id: '400', revision: state.revision, can_edit: true, items: [savedItem], item_count: 1})}}); return true
        }
        if (url.pathname === '/api/manuals/400/files') {
            const body = request.postDataBuffer().toString('latin1')
            const filename = body.match(/filename="([^"]+)"/)?.[1] || ''
            state.uploads.push(filename)
            if (state.uploads.length === 1) { markUploadStarted(); await firstUploadReleased }
            if (filename === 'waiting.png' && state.uploads.filter(value => value === filename).length === 1) {
                await route.fulfill({status: 500, json: {code: 50001, message: '合成慢上传失败'}}); return true
            }
            state.revision++; state.itemSequence++
            await route.fulfill({json: {code: 0, data: {item: {id: `new-${state.itemSequence}`, kind: 'image', title: filename, original_name: filename, position: state.itemSequence + 1, content_url: `/content/${filename}`, thumbnail_url: `/thumb/${filename}`, preview_status: 'ready'}, revision: state.revision}}}); return true
        }
        if (url.pathname === '/api/manuals/400' && request.method() === 'PATCH') {
            const body = request.postDataJSON(); state.writes.push({kind: 'patch', body}); state.revision++
            await route.fulfill({json: {code: 0, data: baseManual({id: '400', name: body.name, revision: state.revision, can_edit: true, items: [], item_count: body.item_ids.length})}}); return true
        }
        if (url.pathname.startsWith('/api/manuals/400') && request.method() === 'DELETE') {
            state.writes.push({kind: 'delete', body: request.postDataJSON()}); await route.fulfill({json: {code: 0, data: {revision: ++state.revision}}}); return true
        }
        return false
    }
    const {page, context} = await setup({path: '/manuals/400/edit?as=owner', state, handler})
    try {
        const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')
        const input = page.getByLabel('选择图片')
        await input.setInputFiles([{name: 'slow.png', mimeType: 'image/png', buffer: png}, {name: 'waiting.png', mimeType: 'image/png', buffer: png}])
        const initialQueue = await page.evaluate(() => window.manualPage.queue.map(item => item.local_id))
        await page.getByRole('button', {name: '上传待处理资料', exact: true}).click()
        await uploadStarted

        assert.equal(await input.isDisabled(), true)
        assert.equal(await page.getByRole('button', {name: '添加文本', exact: true}).isDisabled(), true)
        assert.equal(await page.getByLabel('第 2 项标题').isDisabled(), true)
        assert.equal(await page.getByRole('button', {name: '保存设置与顺序', exact: true}).isDisabled(), true)
        assert.equal(await page.getByRole('button', {name: '删除整份说明书', exact: true}).isDisabled(), true)

        await page.evaluate(() => {
            const editor = window.manualPage
            void editor.saveSettings()
            editor.appendFiles({target: {files: [new File(['late'], 'late.png', {type: 'image/png'})], value: 'chosen'}})
            editor.appendStructured('text')
            editor.moveQueue(1, -1)
            editor.removeQueue(1)
            void editor.deleteSavedItem(editor.manual.items[0])
        })
        await page.waitForTimeout(100)
        assert.deepEqual(await page.evaluate(() => window.manualPage.queue.map(item => item.local_id)), initialQueue)
        assert.equal(state.writes.length, 0)
        assert.equal(await page.locator('.el-message-box').count(), 0)

        releaseFirstUpload()
        await page.getByText('1 项资料上传失败；成功追加项已可见，失败项未保存。', {exact: true}).waitFor()
        assert.deepEqual(state.uploads, ['slow.png', 'waiting.png'])
        assert.equal(state.writes.length, 0)
        await page.getByRole('button', {name: '重试失败项', exact: true}).click()
        await page.getByText('资料已上传，设置与顺序已保存', {exact: true}).waitFor()
        assert.deepEqual(state.uploads, ['slow.png', 'waiting.png', 'waiting.png'])
        assert.equal(state.writes.length, 1)
        assert.deepEqual(state.writes[0].body.item_ids, ['saved-1', 'new-1', 'new-2'])
        assert.equal(state.writes[0].body.revision, 9)
        assert.equal(await page.locator('.upload-queue-item').count(), 0)
    } finally {
        releaseFirstUpload()
        await context.close()
    }
})

test('409 先读取最新版本并保留本地表单，自动封面不误发有效 cover id', async () => {
    const item = {id: '501', kind: 'text', title: '快速入门', text: '原始内容', position: 1}
    const state = {requests: [], writes: [], getCount: 0}
    const handler = async ({route, request, url}) => {
        if (url.pathname === '/api/manuals/categories') { await route.fulfill({json: {code: 0, data: {items: ['厨房']}}}); return true }
        if (url.pathname === '/api/manuals/300' && request.method() === 'GET') {
            state.getCount++
            const latest = state.getCount > 1
            await route.fulfill({json: {code: 0, data: baseManual({id: '300', name: latest ? '远程新名称' : '原名称', revision: latest ? 8 : 7, can_edit: true, cover_item_id: null, cover: {id: '501', kind: 'text', title: '快速入门', text_excerpt: '原始内容', thumbnail_url: '', url_host: '', preview_status: 'none'}, items: [item], item_count: 1})}})
            return true
        }
        if (url.pathname === '/api/manuals/300' && request.method() === 'PATCH') {
            const body = request.postDataJSON(); state.writes.push(body)
            if (state.writes.length === 1) { await route.fulfill({status: 409, json: {code: 40901, message: '修订冲突'}}); return true }
            await route.fulfill({json: {code: 0, data: baseManual({id: '300', name: body.name, categories: body.categories, revision: 9, can_edit: true, cover_item_id: null, items: [item], item_count: 1})}})
            return true
        }
        return false
    }
    const {page, context} = await setup({path: '/manuals/300/edit?as=owner', state, handler})
    try {
        await page.getByLabel('说明书名称').fill('本地草稿名称')
        await page.getByRole('button', {name: '保存设置与顺序', exact: true}).click()
        await page.getByRole('heading', {name: '服务器上已有新版本'}).waitFor()
        assert.equal(await page.getByLabel('说明书名称').inputValue(), '本地草稿名称')
        assert.equal(state.writes.length, 1)
        await page.getByRole('button', {name: '以最新版本继续，保留本地表单', exact: true}).click()
        assert.equal(await page.getByLabel('说明书名称').inputValue(), '本地草稿名称')
        await page.getByRole('button', {name: '保存设置与顺序', exact: true}).click()
        await page.getByText('设置与资料顺序已保存', {exact: true}).waitFor()
        assert.equal(state.writes[1].revision, 8)
        assert.equal('cover_item_id' in state.writes[1], false)
    } finally { await context.close() }
})

test('刷新后提示重新选择未上传的本地文件', async () => {
    const item = {id: '501', kind: 'text', title: '快速入门', text: '原始内容', position: 1}
    const state = {requests: [], writes: []}
    const handler = async ({route, request, url}) => {
        if (url.pathname === '/api/manuals/categories') { await route.fulfill({json: {code: 0, data: {items: []}}}); return true }
        if (url.pathname === '/api/manuals/300' && request.method() === 'GET') {
            await route.fulfill({json: {code: 0, data: baseManual({id: '300', revision: 7, can_edit: true, items: [item], item_count: 1})}}); return true
        }
        return false
    }
    const {page, context} = await setup({path: '/manuals/300/edit?as=owner', width: 320, state, handler})
    try {
        const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')
        await page.getByLabel('选择图片').setInputFiles({name: 'pending.png', mimeType: 'image/png', buffer: png})
        assert.equal(await page.locator('.upload-queue-item').count(), 1)
        await page.reload(); await page.waitForFunction(() => !!window.manualPage)
        await page.getByText('页面刷新后无法恢复尚未上传的本地文件，请重新选择这些文件。', {exact: true}).waitFor()
        assert.equal(await page.locator('.upload-queue-item').count(), 0)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    } finally { await context.close() }
})

test('删除资料和说明书的真实请求体保持数字 revision', async () => {
    const first = {id: 'item-1', kind: 'text', title: '第一项', text: '内容一', position: 1}
    const second = {id: 'item-2', kind: 'text', title: '第二项', text: '内容二', position: 2}
    const state = {requests: [], deletes: [], revision: 7, itemDeleted: false}
    const handler = async ({route, request, url}) => {
        if (url.pathname === '/api/manuals/categories') { await route.fulfill({json: {code: 0, data: {items: []}}}); return true }
        if (url.pathname === '/api/manuals/500' && request.method() === 'GET') {
            const items = state.itemDeleted ? [first] : [first, second]
            await route.fulfill({json: {code: 0, data: baseManual({id: '500', revision: state.revision, can_edit: true, items, item_count: items.length})}}); return true
        }
        if (url.pathname === '/api/manuals/500/items/item-2' && request.method() === 'DELETE') {
            state.deletes.push({kind: 'item', body: request.postDataJSON()}); state.itemDeleted = true; state.revision++
            await route.fulfill({json: {code: 0, data: {revision: state.revision}}}); return true
        }
        if (url.pathname === '/api/manuals/500' && request.method() === 'DELETE') {
            state.deletes.push({kind: 'manual', body: request.postDataJSON()})
            await route.fulfill({json: {code: 0, data: {}}}); return true
        }
        return false
    }
    const {page, context} = await setup({path: '/manuals/500/edit?as=owner', state, handler})
    try {
        await page.evaluate(() => { window.manualPage.$.appContext.config.globalProperties.$confirm = () => Promise.resolve() })
        const savedSecond = page.locator('.saved-item').filter({hasText: '第二项'})
        await savedSecond.getByRole('button', {name: '删除', exact: true}).click()
        await page.getByText('资料已删除', {exact: true}).waitFor()
        await page.getByRole('button', {name: '删除整份说明书', exact: true}).click()
        await page.waitForFunction(() => window.fixtureRoute.path === '/manuals')

        assert.deepEqual(state.deletes, [
            {kind: 'item', body: {revision: 7}},
            {kind: 'manual', body: {revision: 8}},
        ])
        assert.equal(typeof state.deletes[0].body.revision, 'number')
        assert.equal(typeof state.deletes[1].body.revision, 'number')
    } finally { await context.close() }
})

test('编辑器为待上传和已保存文件显示缩略图，排序保持预览对应并释放移除项 URL', async () => {
    const savedImage = {id: 'image-1', kind: 'image', title: '正面', original_name: 'front.png', position: 1, thumbnail_url: '/thumb/saved-image.png', preview_status: 'ready'}
    const savedPDF = {id: 'pdf-1', kind: 'pdf', title: '完整手册', original_name: 'guide.pdf', position: 2, thumbnail_url: '/thumb/saved-pdf.png', preview_status: 'ready'}
    const handler = async ({route, request, url}) => {
        if (url.pathname === '/api/manuals/categories') { await route.fulfill({json: {code: 0, data: {items: []}}}); return true }
        if (url.pathname === '/api/manuals/500' && request.method() === 'GET') {
            await route.fulfill({json: {code: 0, data: baseManual({id: '500', can_edit: true, items: [savedImage, savedPDF], item_count: 2})}}); return true
        }
        return false
    }
    const {page, context} = await setup({path: '/manuals/500/edit?as=owner', width: 320, handler})
    try {
        await page.evaluate(() => {
            window.__revokedManualPreviewURLs = []
            const revoke = URL.revokeObjectURL.bind(URL)
            URL.revokeObjectURL = value => { window.__revokedManualPreviewURLs.push(value); revoke(value) }
        })
        const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=', 'base64')
        await page.getByLabel('选择图片').setInputFiles({name: 'local.png', mimeType: 'image/png', buffer: png})
        await page.getByLabel('选择 PDF 或 TXT').setInputFiles({name: 'local-manual', mimeType: 'application/pdf', buffer: fixturePDF()})
        await page.getByLabel('选择 PDF 或 TXT').setInputFiles({name: 'attack.pdf', mimeType: 'text/html', buffer: Buffer.from('<script>parent.__manualEditorXSS = true</script>')})

        assert.equal(await page.locator('.queue-preview-image').count(), 1)
        assert.match(await page.locator('.queue-preview-image').getAttribute('src'), /^blob:/)
        const localPDFCard = page.locator('.upload-queue-item').filter({hasText: 'local-manual'})
        assert.equal(await localPDFCard.locator('.queue-preview-pdf').count(), 1)
        assert.match(await localPDFCard.locator('.queue-preview-pdf').getAttribute('src'), /^blob:/)
        const attackPDFCard = page.locator('.upload-queue-item').filter({hasText: 'attack.pdf'})
        assert.equal(await attackPDFCard.locator('.queue-preview-pdf').count(), 0)
        assert.equal(await page.evaluate(() => window.__manualEditorXSS), undefined)
        assert.equal(await page.locator('.saved-thumbnail').count(), 2)
        const savedPDFCard = page.locator('.saved-item').filter({hasText: '完整手册'})
        await savedPDFCard.getByRole('button', {name: '上移', exact: true}).click()
        assert.match(await page.locator('.saved-item').first().locator('.saved-thumbnail').getAttribute('src'), /saved-pdf\.png$/)

        const pdfCard = page.locator('.upload-queue-item').filter({hasText: 'local-manual'})
        const pdfPreview = await pdfCard.locator('.queue-preview-pdf').getAttribute('src')
        await pdfCard.getByRole('button', {name: '上移', exact: true}).click()
        assert.match(await page.locator('.upload-queue-item').first().innerText(), /local-manual/)
        assert.equal(await page.locator('.upload-queue-item').first().locator('.queue-preview-pdf').getAttribute('src'), pdfPreview)
        await page.locator('.upload-queue-item').first().getByRole('button', {name: '移除', exact: true}).click()
        assert.equal(await page.evaluate(() => window.__revokedManualPreviewURLs.length), 1)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    } finally { await context.close() }
})

test('匿名详情按顺序安全展示图片、PDF、纯文本和 HTTP(S) 链接', async () => {
    const manual = baseManual({items: [
        {id: '1', kind: 'image', title: '正面', content_url: '/content/front.png', thumbnail_url: '/thumb/front.png', preview_status: 'ready'},
        {id: '2', kind: 'pdf', title: '完整手册', original_name: 'guide.pdf', content_url: '/content/guide.pdf', preview_status: 'ready'},
        {id: '3', kind: 'text', title: '保养', text: '<script>window.__manualXSS=1</script>\n用清水冲洗'},
        {id: '4', kind: 'url', title: '官网支持', url: 'https://example.com/help'},
        {id: '5', kind: 'url', title: '无效链接', url: 'javascript:alert(1)'},
    ], item_count: 5})
    const handler = async ({route, request, url}) => {
        if (url.pathname === '/api/manuals/200' && request.method() === 'GET') { await route.fulfill({json: {code: 0, data: manual}}); return true }
        return false
    }
    const {page, context} = await setup({path: '/manuals/200', width: 320, handler})
    try {
        await page.getByText('用清水冲洗', {exact: false}).waitFor()
        assert.equal(await page.evaluate(() => window.__manualXSS), undefined)
        await page.locator('.manual-image img').click()
        await page.locator('.el-image-viewer__wrapper').waitFor()
        await page.keyboard.press('Escape')
        const external = page.getByRole('link', {name: '打开官网支持'})
        assert.equal(await external.getAttribute('target'), '_blank')
        assert.match(await external.getAttribute('rel'), /noopener/)
        assert.match(await external.getAttribute('rel'), /noreferrer/)
        assert.equal(await page.getByRole('link', {name: '打开无效链接'}).count(), 0)
        const pdfCard = page.locator('.manual-item').filter({hasText: '完整手册'})
        assert.equal(await pdfCard.locator('.item-meta').textContent(), '资料 2 · PDF')
        assert.equal(await pdfCard.locator('.item-filename').textContent(), 'guide.pdf')
        assert.equal(await page.locator('.item-heading > span').count(), 0)
        assert.equal(await pdfCard.locator('iframe.pdf-viewer').count(), 1)
        assert.match(await pdfCard.locator('iframe.pdf-viewer').getAttribute('src'), /\/content\/guide\.pdf$/)
        assert.equal(await pdfCard.locator('iframe.pdf-viewer').getAttribute('title'), '完整手册 PDF 阅读器')
        if (!page.frames().some(frame => frame.url().startsWith('chrome-extension://mhjfbmdgcfjbbpaeojofohoefgiehjai/'))) {
            await page.waitForEvent('framenavigated', {predicate: frame => frame.url().startsWith('chrome-extension://mhjfbmdgcfjbbpaeojofohoefgiehjai/')})
        }
        assert.equal(await page.getByRole('link', {name: '打开 PDF'}).getAttribute('target'), '_blank')
        assert.match(await page.getByRole('link', {name: '下载 PDF'}).getAttribute('href'), /download=1/)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    } finally { await context.close() }
})
