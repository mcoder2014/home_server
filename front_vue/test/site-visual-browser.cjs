// 用真实 Vue / Element Plus / Chromium 核验全站公共布局；网络仅访问本地合成 fixture。
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
const visualOutput = process.env.VISUAL_OUTPUT_DIR || '/tmp/home-server-file-sharing-visuals'
let browser, server, origin

const artifacts = {
    '/vue.js': fs.readFileSync(require.resolve('vue/dist/vue.global.prod.js')),
    '/element.js': fs.readFileSync(require.resolve('element-plus/dist/index.full.js')),
    '/icons.js': fs.readFileSync(require.resolve('@element-plus/icons-vue/dist/index.iife.js')),
    '/element.css': fs.readFileSync(require.resolve('element-plus/dist/index.css')),
    '/axios.js': fs.readFileSync(require.resolve('axios/dist/axios.js')),
}
let script = `const __modules={axios:window.axios};
const {ArrowRight,Document,FolderOpened,Key,Lock,Monitor,Plus,Reading,Refresh,User}=ElementPlusIconsVue;
const ElMessage=ElementPlus.ElMessage;
`
const moduleFiles = [
    ['api/browser_client.cjs', ['./browser_client.cjs', '@/api/browser_client.cjs']],
    ['utils/accounts_behavior.cjs', ['@/utils/accounts_behavior.cjs']],
    ['utils/web_projects_navigation.cjs', ['@/utils/web_projects_navigation.cjs']],
    ['api/accounts.cjs', ['@/api/accounts.cjs']],
    ['api/manuals.cjs', ['@/api/manuals.cjs']],
    ['api/web_projects.cjs', ['@/api/web_projects.cjs']],
]
for (const [name, keys] of moduleFiles) {
    script += `(function(){const module={exports:{}};const require=name=>__modules[name];\n${fs.readFileSync(path.join(root, name), 'utf8')}\n${keys.map(key => `__modules[${JSON.stringify(key)}]=module.exports;`).join('')}\n})();\n`
}
let css = fs.readFileSync(path.join(root, 'assets/global.css'), 'utf8')
for (const [folder, name] of [['components', 'UserIdentity'], ['components', 'MyHeader'], ['views', 'Index'], ['views', 'WebProjectList'], ['views', 'ManualList'], ['views', 'Login']]) {
    const {descriptor} = parse(fs.readFileSync(path.join(root, folder, name + '.vue'), 'utf8'))
    const scope = 'data-v-site-' + name.toLowerCase()
    const compiled = compileTemplate({source: descriptor.template.content, id: scope, compilerOptions: {mode: 'function'}})
    assert.deepEqual(compiled.errors, [])
    const component = descriptor.script.content.replace(/^import .*$/gm, '').replace('export default', 'const definition =')
    script += `const ${name}=(function(){const require=name=>__modules[name];${component};definition.render=new Function('Vue',${JSON.stringify(compiled.code)})(Vue);definition.__scopeId=${JSON.stringify(scope)};return definition;})();\n`
    for (const style of descriptor.styles) css += compileStyle({source: style.content, id: scope, scoped: style.scoped}).code
}
script += `
const login=location.pathname==='/login';
const activeUser={id:'42',status:'active',role:'admin',user_name:'owner',display_name:'文件主人',csrf_token:'fixture-csrf',library_enabled:true,capabilities:{manuals:true,applications:true}};
const state=Vue.reactive({userInfo:login?null:activeUser,site:{title:'CQ Home Server',notice:''},registration:{enabled:false},modules:{manuals:true,file_sharing:true},sessionLoaded:true});
const store={state,commit(name,value){if(name==='REMOVE_INFO')state.userInfo=null;else if(name==='SET_USERINFO')state.userInfo=value;},dispatch(){return Promise.resolve({site:state.site,modules:state.modules});}};
__modules['@/api/browser_client.cjs'].configureBrowserSession(()=>state.userInfo?.csrf_token||'',()=>store.commit('REMOVE_INFO'));
const query=Object.fromEntries(new URLSearchParams(location.search)),route=Vue.reactive({path:location.pathname,fullPath:location.pathname+location.search,query,hash:location.hash}),navigations=[];
const router={push(value){navigations.push(value);route.path=typeof value==='string'?value:value.path;return Promise.resolve();},replace(value){navigations.push(value);route.path=typeof value==='string'?value:value.path;return Promise.resolve();}};
const RouterLink={props:['to'],setup(props,{slots,attrs}){return()=>Vue.h('a',{...attrs,href:typeof props.to==='string'?props.to:props.to.path},slots.default?slots.default():[])}};
const component=login?Login:location.pathname==='/web-share'?WebProjectList:location.pathname==='/manuals'?ManualList:Index;
const app=Vue.createApp({render(){return Vue.h(component,{ref:'page'});},mounted(){window.sitePage=this.$refs.page;}});
app.use(ElementPlus);app.component('RouterLink',RouterLink);Object.assign(app.config.globalProperties,{$store:store,$route:route,$router:router,$message:ElementPlus.ElMessage,$confirm:ElementPlus.ElMessageBox.confirm});app.mount('#app');
window.fixtureStore=store;window.fixtureNavigations=navigations;
`
artifacts['/site.js'] = Buffer.from(script)
artifacts['/site.css'] = Buffer.from(css)
const html = '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/element.css"><link rel="stylesheet" href="/site.css"><body><div id="app"></div><script src="/vue.js"></script><script src="/element.js"></script><script src="/icons.js"></script><script src="/axios.js"></script><script src="/site.js"></script></body></html>'

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

async function setup(routePath, width) {
    const context = await browser.newContext({viewport: {width, height: width === 320 ? 820 : 960}})
    const page = await context.newPage(); page.setDefaultTimeout(10000)
    const errors = []; page.on('pageerror', error => errors.push(error.message))
    await page.route('**/api/**', async route => {
        const request = route.request(), url = new URL(request.url())
        let data
        if (url.pathname === '/api/web-share') data = {items: [
            {id: '91', name: '家庭仪表盘', url: '/p/home-dashboard/', access_mode: 'public', status: 'enabled', moderation_status: 'normal', update_time: '2026-09-19 09:30'},
            {id: '92', name: '设备状态页', url: '/p/device-status/', access_mode: 'members', status: 'enabled', moderation_status: 'normal', update_time: '2026-09-18 18:20'},
        ], next_cursor: '', has_more: false}
        else if (url.pathname === '/api/manuals') data = {items: [
            {id: '200', name: '咖啡机', description: '清洁、除垢与常用配方', categories: ['厨房', '家电'], access_mode: 'public', password_protected: true, item_count: 4, update_time: '2026-09-19T03:00:00Z', cover: {kind: 'text', title: '快速入门', text_excerpt: '先清洗滤网，再加入咖啡豆。'}},
            {id: '201', name: '家庭路由器', description: '网络设置与故障排查', categories: ['网络'], access_mode: 'authenticated', password_protected: false, item_count: 3, update_time: '2026-09-18T03:00:00Z', cover: {kind: 'url', title: '管理入口', url_host: 'router.home'}},
            {id: '202', name: '客厅空调', description: '遥控器与滤网说明', categories: ['客厅'], access_mode: 'public', password_protected: false, item_count: 2, update_time: '2026-09-17T03:00:00Z', cover: null},
        ], next_cursor: '', has_more: false}
        else if (url.pathname === '/api/manuals/categories') data = {items: ['厨房', '家电', '网络', '客厅'], next_cursor: '', has_more: false}
        else { await route.fulfill({status: 404, json: {code: 4, message: 'fixture not found'}}); return }
        await route.fulfill({json: {code: 0, data}})
    })
    await page.goto(origin + routePath)
    await page.waitForFunction(() => !!window.sitePage).catch(() => { throw new Error(`site page did not mount: ${errors.join('; ')}`) })
    assert.deepEqual(errors, [])
    return {page, context}
}

test('首页、网页列表、说明书列表在1440与320下无页面级水平溢出并生成截图', async () => {
    fs.mkdirSync(visualOutput, {recursive: true})
    const pages = [
        {route: '/', marker: '常用服务', name: 'home'},
        {route: '/web-share', marker: '家庭仪表盘', name: 'web-list'},
        {route: '/manuals', marker: '咖啡机', name: 'manual-list'},
    ]
    for (const item of pages) {
        const {page, context} = await setup(item.route, 1440)
        try {
            await page.getByText(item.marker, {exact: true}).first().waitFor()
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
            await page.screenshot({path: path.join(visualOutput, `${item.name}-desktop-1440.png`), fullPage: true})
            await page.setViewportSize({width: 320, height: 820})
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
            await page.screenshot({path: path.join(visualOutput, `${item.name}-mobile-320.png`), fullPage: true})
        } finally { await context.close() }
    }
})

test('登录页在320下无水平溢出并生成截图', async () => {
    fs.mkdirSync(visualOutput, {recursive: true})
    const {page, context} = await setup('/login', 320)
    try {
        await page.getByText('登录你的空间', {exact: true}).waitFor()
        assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth), false)
        await page.screenshot({path: path.join(visualOutput, 'login-mobile-320.png'), fullPage: true})
    } finally { await context.close() }
})
