// 用真实 Vue / Element Plus / Chromium 验证文件管理与收件页；网络仅访问本地合成 fixture。
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
const shareToken = 'AbCDef0123456789_-AbCDef01234567'
let browser, server, origin
const visualOutput = process.env.VISUAL_OUTPUT_DIR || '/tmp/home-server-file-sharing-visuals'
const artifacts = {
    '/vue.js': fs.readFileSync(require.resolve('vue/dist/vue.global.prod.js')),
    '/element.js': fs.readFileSync(require.resolve('element-plus/dist/index.full.js')),
    '/element.css': fs.readFileSync(require.resolve('element-plus/dist/index.css')),
    '/axios.js': fs.readFileSync(require.resolve('axios/dist/axios.js')),
}
let script = 'const __modules={axios:window.axios};const Loading={render(){return null}};\n'
for (const name of ['api/browser_client.cjs', 'utils/accounts_behavior.cjs', 'utils/file_sharing_behavior.cjs', 'utils/web_projects_navigation.cjs', 'api/accounts.cjs', 'api/files.cjs', 'api/web_projects.cjs']) {
    const keys = {
        'api/browser_client.cjs': ['./browser_client.cjs', '@/api/browser_client.cjs'],
        'utils/accounts_behavior.cjs': ['@/utils/accounts_behavior.cjs'],
        'utils/file_sharing_behavior.cjs': ['@/utils/file_sharing_behavior.cjs'],
        'utils/web_projects_navigation.cjs': ['@/utils/web_projects_navigation.cjs'],
        'api/accounts.cjs': ['@/api/accounts.cjs'],
        'api/files.cjs': ['@/api/files.cjs'],
        'api/web_projects.cjs': ['@/api/web_projects.cjs'],
    }[name]
    script += `(function(){const module={exports:{}};const require=name=>__modules[name];\n${fs.readFileSync(path.join(root, name), 'utf8')}\n${keys.map(key => `__modules[${JSON.stringify(key)}]=module.exports;`).join('')}\n})();\n`
}
let css = fs.readFileSync(path.join(root, 'assets/global.css'), 'utf8')
for (const name of ['UserIdentity', 'MyHeader']) {
    const {descriptor} = parse(fs.readFileSync(path.join(root, 'components', name + '.vue'), 'utf8'))
    const scope = 'data-v-files-' + name.toLowerCase()
    const compiled = compileTemplate({source: descriptor.template.content, id: scope, compilerOptions: {mode: 'function'}})
    assert.deepEqual(compiled.errors, [])
    const component = descriptor.script.content.replace(/^import .*$/gm, '').replace('export default', 'const definition =')
    script += `const ${name}=(function(){const require=name=>__modules[name];${component};definition.render=new Function('Vue',${JSON.stringify(compiled.code)})(Vue);definition.__scopeId=${JSON.stringify(scope)};return definition;})();\n`
    for (const style of descriptor.styles) css += compileStyle({source: style.content, id: scope, scoped: style.scoped}).code
}
for (const name of ['FileShareManager', 'FileShareReceive', 'WebProjectOpen']) {
    const {descriptor} = parse(fs.readFileSync(path.join(root, 'views', name + '.vue'), 'utf8'))
    const scope = 'data-v-files-' + name.toLowerCase()
    const compiled = compileTemplate({source: descriptor.template.content, id: scope, compilerOptions: {mode: 'function'}})
    assert.deepEqual(compiled.errors, [])
    const component = descriptor.script.content.replace(/^import .*$/gm, '').replace('export default', 'const definition =')
    script += `const ${name}=(function(){const require=name=>__modules[name];${component};definition.render=new Function('Vue',${JSON.stringify(compiled.code)})(Vue);definition.__scopeId=${JSON.stringify(scope)};return definition;})();\n`
    for (const style of descriptor.styles) css += compileStyle({source: style.content, id: scope, scoped: style.scoped}).code
}
script += `
const segments=location.pathname.split('/').filter(Boolean),recipient=segments[0]==='s',webOpen=location.pathname==='/web-share/open';
const state=Vue.reactive({userInfo:recipient?null:{id:'42',status:'active',user_name:'owner',display_name:'文件主人',csrf_token:'fixture-csrf',capabilities:{manuals:true},library_enabled:true},site:{title:'合成家庭服务'},modules:{manuals:true,file_sharing:true},sessionLoaded:true});
const store={state,commit(name,value){if(name==='REMOVE_INFO')state.userInfo=null;else if(name==='SET_USERINFO')state.userInfo=value;},dispatch(){return Promise.resolve();}};
__modules['@/api/browser_client.cjs'].configureBrowserSession(()=>state.userInfo?.csrf_token||'',()=>store.commit('REMOVE_INFO'));
const route=Vue.reactive({path:location.pathname,fullPath:location.pathname+location.search,hash:location.hash,params:{token:segments[1]||''},query:Object.fromEntries(new URLSearchParams(location.search))}),navigations=[];
const router={push(value){navigations.push(value);route.path=typeof value==='string'?value:value.path;},replace(value){navigations.push(value);route.path=typeof value==='string'?value:value.path;}};
const RouterLink={props:['to'],setup(props,{slots,attrs}){return()=>Vue.h('a',{...attrs,href:typeof props.to==='string'?props.to:props.to.path},slots.default?slots.default():[])}};
const component=recipient?FileShareReceive:webOpen?WebProjectOpen:FileShareManager;
const app=Vue.createApp({render(){return Vue.h(component,{ref:'page'});},mounted(){window.filePage=this.$refs.page;}});
app.use(ElementPlus);app.component('RouterLink',RouterLink);Object.assign(app.config.globalProperties,{$store:store,$route:route,$router:router,$message:ElementPlus.ElMessage,$confirm:ElementPlus.ElMessageBox.confirm});app.mount('#app');
window.fixtureStore=store;window.fixtureNavigations=navigations;
`
artifacts['/files.js'] = Buffer.from(script)
artifacts['/files.css'] = Buffer.from(css)
const html = '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/element.css"><link rel="stylesheet" href="/files.css"><body><div id="app"></div><script src="/vue.js"></script><script src="/element.js"></script><script src="/axios.js"></script><script src="/files.js"></script></body></html>'

before(async () => {
    server = http.createServer((req, res) => {
        const artifact = artifacts[req.url]
        res.setHeader('Content-Type', artifact ? req.url.endsWith('.css') ? 'text/css' : 'application/javascript' : 'text/html; charset=utf-8')
        res.end(artifact || html)
    })
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve))
    origin = 'http://127.0.0.1:' + server.address().port
    const chrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
    const executablePath = process.env.CHROME_PATH || (process.platform === 'darwin' && fs.existsSync(chrome) ? chrome : undefined)
    browser = await chromium.launch({headless: true, ...(executablePath ? {executablePath} : {})})
})
after(async () => { if (browser) await browser.close(); if (server) await new Promise(resolve => server.close(resolve)) })

function fileFixture(overrides = {}) {
    return {id: '10', name: '家庭账单.pdf', size_bytes: 1048576, sha256: 'a'.repeat(64), share_count: 2, create_time: '2026-09-19T01:00:00Z', ...overrides}
}
function shareFixture(overrides = {}) {
    return {id: '20', file_id: '10', token: shareToken, url: `/s/${shareToken}`, access_mode: 'public', member_user_ids: [], secret_mode: 'none', expires_at: null, max_downloads: 0, download_count: 2, revoked_at: null, create_time: '2026-09-19T02:00:00Z', ...overrides}
}

async function setup(options = {}) {
    const secondToken = shareToken.slice(0, -1) + '8'
    const state = options.state || {requests: [], writes: [], downloads: 0, unlocked: false, files: [fileFixture()], shares: [shareFixture(), shareFixture({id: '21', token: secondToken, url: `/s/${secondToken}`, secret_mode: 'code', expires_at: '2026-09-26T00:00:00Z', max_downloads: 3, download_count: 1})]}
    const context = await browser.newContext({viewport: {width: options.width || 1280, height: options.height || 950}, acceptDownloads: true})
    const page = await context.newPage(); page.setDefaultTimeout(10000)
    const errors = []; page.on('pageerror', error => errors.push(error.message))
    await page.route('**/p/**', async route => {
        const request = route.request(), url = new URL(request.url())
        state.requests.push({method: request.method(), path: url.pathname})
        await route.fulfill(state.projectUnlocked ? {status: 200} : {status: 403, headers: {'x-resource-password': 'required', 'x-resource-id': '123'}})
    })
    await page.route('**/api/**', async route => {
        const request = route.request(), url = new URL(request.url()), method = request.method()
        state.requests.push({method, path: url.pathname})
        if (options.handler && await options.handler({route, request, url, state})) return
        let data
        if (url.pathname === '/api/auth/me' && method === 'GET') data = {id: '42', status: 'active'}
        else if (url.pathname === '/api/web-share/123/unlock' && method === 'POST') { state.projectUnlocked = true; data = {password_protected: true, version: 1} }
        else if (url.pathname === '/api/files' && method === 'GET') data = {items: state.files, next_cursor: '', has_more: false}
        else if (url.pathname === '/api/files' && method === 'POST') {
            const file = fileFixture({id: '11', name: '新上传.txt', size_bytes: 12, share_count: 0}); state.files.push(file); data = file
        } else if (url.pathname === '/api/files/10' && method === 'DELETE') {
            state.writes.push({kind: 'delete-file'}); data = {id: '10', deleted_at: '2026-09-19T04:30:00Z'}
        } else if (url.pathname === '/api/files/eligible-users') data = {items: [{id: '7', user_name: 'member', display_name: '家庭成员'}]}
        else if (url.pathname === '/api/files/10/shares' && method === 'GET') data = {items: state.shares, next_cursor: '', has_more: false}
        else if (url.pathname === '/api/files/10/shares' && method === 'POST') {
            const body = request.postDataJSON(); state.writes.push({kind: 'create-share', body})
            const createdToken = shareToken.slice(0, -1) + '9'
            const share = shareFixture({id: '22', token: createdToken, url: `/s/${createdToken}`, ...body, download_count: 0}); state.shares.unshift(share); data = {share, secret: body.secret || 'Server9'}
        } else if (url.pathname === '/api/files/10/shares/20' && method === 'DELETE') {
            state.writes.push({kind: 'revoke'}); state.shares[0].revoked_at = '2026-09-19T04:00:00Z'; data = {id: '20', revoked_at: state.shares[0].revoked_at}
        } else if (url.pathname === `/api/file-shares/${shareToken}` && method === 'GET') {
            data = state.unlocked || options.open
                ? {state: 'available', file: {name: '家庭账单.pdf', size_bytes: 1048576}, expires_at: '2026-09-26T00:00:00Z', max_downloads: 3, download_count: 1, remaining_downloads: 2, secret_mode: options.open ? 'none' : 'code', access_mode: 'public'}
                : {state: 'locked', secret_mode: 'code', access_mode: 'public'}
        } else if (url.pathname === `/api/file-shares/${shareToken}/unlock`) {
            state.writes.push({kind: 'unlock', body: request.postDataJSON()}); state.unlocked = true; data = {unlocked_until: '2026-09-19T05:00:00Z'}
        } else if (url.pathname === `/api/file-shares/${shareToken}/download`) {
            state.downloads++; await route.fulfill({status: 200, headers: {'content-type': 'application/octet-stream', 'content-disposition': "attachment; filename*=UTF-8''%E5%AE%B6%E5%BA%AD%E8%B4%A6%E5%8D%95.pdf"}, body: 'file bytes'}); return
        } else { await route.fulfill({status: 404, json: {code: 4, message: 'fixture not found'}}); return }
        await route.fulfill({json: {code: 0, data}})
    })
    await page.goto(origin + (options.path || '/files'))
    await page.waitForFunction(() => !!window.filePage).catch(() => { throw new Error(`file page did not mount; ${errors.join('; ')}`) })
    assert.deepEqual(errors, [])
    return {page, context, state, errors}
}

test('文件管理在桌面创建叠加限制的分享，并显示多条独立记录及一次性口令', async () => {
    const {page, context, state} = await setup({width: 1440, height: 1000})
    try {
        await page.getByText('家庭账单.pdf', {exact: true}).first().waitFor()
        await page.getByRole('button', {name: '管理分享', exact: true}).click()
        await page.locator('.share-record').first().waitFor()
        assert.equal(await page.locator('.share-record').count(), 2)
        await page.getByRole('button', {name: '新建分享', exact: true}).click()
        await page.getByLabel('需要分享码').check()
        await page.getByLabel('随机生成').check()
        const generated = await page.getByRole('textbox', {name: '6 位分享码', exact: true}).inputValue()
        assert.match(generated, /^[A-Za-z0-9]{6}$/)
        await page.getByLabel('有效期').selectOption('7d')
        await page.getByLabel('下载次数').selectOption('custom')
        await page.getByLabel('最多下载次数').fill('3')
        await page.getByRole('button', {name: '创建分享链接', exact: true}).click()
        await page.getByText('分享链接已创建', {exact: true}).waitFor()
        const body = state.writes.find(item => item.kind === 'create-share').body
        assert.equal(body.access_mode, 'public')
        assert.equal(body.secret_mode, 'code')
        assert.equal(body.secret, generated)
        assert.equal(body.max_downloads, 3)
        assert.ok(new Date(body.expires_at) > new Date())
        assert.equal(await page.getByText(generated, {exact: true}).count(), 1)
        await page.evaluate(() => scrollTo(0, 0))
        fs.mkdirSync(visualOutput, {recursive: true})
        await page.screenshot({path: path.join(visualOutput, 'file-sharing-desktop-1440.png'), fullPage: true})
    } finally { await context.close() }
})

test('320px 文件页可上传、撤销分享且没有水平溢出', async () => {
    const {page, context, state} = await setup({width: 320})
    try {
        await page.getByLabel('选择文件').setInputFiles({name: '新上传.txt', mimeType: 'text/plain', buffer: Buffer.from('hello world!')})
        await page.getByRole('button', {name: '上传文件', exact: true}).click()
        await page.getByText('新上传.txt', {exact: true}).waitFor()
        await page.getByRole('button', {name: '管理分享', exact: true}).first().click()
        await page.locator('.share-record').first().getByRole('button', {name: '撤销', exact: true}).click()
        const dialog = page.getByRole('dialog', {name: '撤销分享链接', exact: true})
        await dialog.getByRole('button', {name: '确认撤销', exact: true}).click()
        await dialog.waitFor({state: 'hidden'})
        await page.getByText('已撤销', {exact: true}).waitFor()
        assert.equal(state.writes.some(item => item.kind === 'revoke'), true)
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
        await page.locator('.el-message').last().waitFor({state: 'hidden'})
        await page.evaluate(() => scrollTo(0, 0))
        fs.mkdirSync(visualOutput, {recursive: true})
        await page.screenshot({path: path.join(visualOutput, 'file-sharing-mobile-320.png'), fullPage: true})
    } finally { await context.close() }
})

test('删除文件确认后只发送一次 DELETE', async () => {
    const {page, context, state} = await setup()
    try {
        await page.getByText('家庭账单.pdf', {exact: true}).first().waitFor()
        await page.getByRole('button', {name: '删除', exact: true}).click()
        const dialog = page.getByRole('dialog', {name: '删除文件', exact: true})
        await dialog.getByRole('button', {name: '确认删除', exact: true}).click()
        await page.getByText('家庭账单.pdf', {exact: true}).first().waitFor({state: 'detached'})
        assert.equal(state.writes.filter(item => item.kind === 'delete-file').length, 1)
    } finally { await context.close() }
})

test('收件页在1440与320展示通用口令门禁，解锁后连点只发送一次下载 POST', async () => {
    const {page, context, state} = await setup({path: `/s/${shareToken}`, width: 1440, height: 900})
    try {
        await page.getByText('受保护文件', {exact: true}).waitFor()
        fs.mkdirSync(visualOutput, {recursive: true})
        await page.screenshot({path: path.join(visualOutput, 'file-receive-password-desktop-1440.png'), fullPage: true})
        await page.setViewportSize({width: 320, height: 820})
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
        await page.screenshot({path: path.join(visualOutput, 'file-receive-password-mobile-320.png'), fullPage: true})
        await page.getByLabel('6 位分享码').fill('aB09zZ')
        await page.getByRole('button', {name: '解锁文件', exact: true}).click()
        await page.getByRole('button', {name: '下载文件', exact: true}).waitFor()
        const downloadPromise = page.waitForEvent('download')
        await page.getByRole('button', {name: '下载文件', exact: true}).dblclick()
        const download = await downloadPromise
        assert.equal(download.suggestedFilename(), '家庭账单.pdf')
        assert.equal(state.downloads, 1)
        assert.deepEqual(state.writes.find(item => item.kind === 'unlock').body, {secret: 'aB09zZ'})
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
    } finally { await context.close() }
})

test('无密码分享不会自动消耗次数，点击后只下载一次并可生成桌面视觉截图', async () => {
    const {page, context, state} = await setup({path: `/s/${shareToken}`, open: true, width: 1440, height: 900})
    try {
        await page.getByRole('button', {name: '下载文件', exact: true}).waitFor()
        assert.equal(state.downloads, 0)
        fs.mkdirSync(visualOutput, {recursive: true})
        await page.screenshot({path: path.join(visualOutput, 'file-receive-available-desktop-1440.png'), fullPage: true})
        const downloadPromise = page.waitForEvent('download')
        await page.getByRole('button', {name: '下载文件', exact: true}).click()
        await downloadPromise
        assert.equal(state.downloads, 1)
    } finally { await context.close() }
})

test('网页阅读密码门禁在1440与320下清晰可用', async () => {
    const state = {requests: [], writes: [], downloads: 0, unlocked: false, projectUnlocked: false, files: [fileFixture()], shares: []}
    const {page, context} = await setup({path: '/web-share/open?target=%2Fp%2Freport%2F&project=123', width: 1440, height: 900, state})
    try {
        await page.getByText('输入阅读密码', {exact: true}).waitFor()
        fs.mkdirSync(visualOutput, {recursive: true})
        await page.screenshot({path: path.join(visualOutput, 'web-password-desktop-1440.png'), fullPage: true})
        await page.setViewportSize({width: 320, height: 820})
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
        await page.screenshot({path: path.join(visualOutput, 'web-password-mobile-320.png'), fullPage: true})
    } finally { await context.close() }
})
