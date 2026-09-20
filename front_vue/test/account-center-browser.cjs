// 用真实 Vue / Element Plus / Chromium 验证 SFC；仅访问本地合成 HTTP fixture。
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
const visualOutput = process.env.VISUAL_OUTPUT_DIR || '/tmp/home-server-file-sharing-visuals'
const artifacts = {
  '/vue.js': fs.readFileSync(require.resolve('vue/dist/vue.global.prod.js')),
  '/element.js': fs.readFileSync(require.resolve('element-plus/dist/index.full.js')),
  '/element.css': fs.readFileSync(require.resolve('element-plus/dist/index.css')),
  '/axios.js': fs.readFileSync(require.resolve('axios/dist/axios.js')),
}
let script = 'const __modules={axios:window.axios}; const {markRaw}=Vue;\n'
for (const name of ['utils/accounts_behavior.cjs', 'utils/account_updates.cjs', 'api/browser_client.cjs', 'api/accounts.cjs']) {
  const key = name === 'api/browser_client.cjs' ? './browser_client.cjs' : '@/' + name
  script += `(function(){const module={exports:{}};const require=name=>__modules[name];\n${fs.readFileSync(path.join(root, name), 'utf8')}\n__modules[${JSON.stringify(key)}]=module.exports;})();\n`
}
let css = fs.readFileSync(path.join(root, 'assets/global.css'), 'utf8')
script += "__modules['@/api/browser_client.cjs']=__modules['./browser_client.cjs'];\n"
for (const name of ['UserIdentity', 'SessionDetails', 'LoginSessions', 'AvatarCropper', 'MyHeader', 'AdminConfirm', 'Account', 'AdminUsers', 'AdminConfig']) {
  const folder = ['Account', 'AdminUsers', 'AdminConfig'].includes(name) ? 'views' : 'components'
  const {descriptor} = parse(fs.readFileSync(path.join(root, folder, name + '.vue'), 'utf8'))
  const scope = 'data-v-center-' + name.toLowerCase()
  const compiled = compileTemplate({source: descriptor.template.content, id: scope, compilerOptions: {mode: 'function'}})
  assert.deepEqual(compiled.errors, [])
  const component = descriptor.script.content.replace(/^import .*$/gm, '').replace('export default', 'const definition =')
  script += `const ${name}=(function(){const require=name=>__modules[name];${component};definition.render=new Function('Vue',${JSON.stringify(compiled.code)})(Vue);definition.__scopeId=${JSON.stringify(scope)};return definition;})();\n`
  for (const style of descriptor.styles) css += compileStyle({source: style.content, id: scope, scoped: style.scoped}).code
}
script += `
const {accountsApi}=__modules['@/api/accounts.cjs'];
const state=Vue.reactive({userInfo:null,site:{title:'合成测试站'},sessionLoaded:true});
let updates;
const store={state,commit(name,value){if(name==='SET_USERINFO')state.userInfo=value;else if(name==='REMOVE_INFO')state.userInfo=null;},async dispatch(name){if(name==='refreshSession'){const user=await accountsApi.me();store.commit('SET_USERINFO',user);return user;}if(name==='publishProfileUpdate')updates.publish();}};
__modules['@/api/browser_client.cjs'].configureBrowserSession(()=>state.userInfo?.csrf_token||'',()=>store.commit('REMOVE_INFO'));
updates=__modules['@/utils/account_updates.cjs'].startAccountUpdates(window,()=>store.dispatch('refreshSession'));
const route=Vue.reactive({path:location.pathname,query:{}}),router={replace(value){route.path=typeof value==='string'?value:value.path;},push(value){route.path=typeof value==='string'?value:value.path;}};
store.dispatch('refreshSession').then(()=>{
 const page=location.pathname.startsWith('/admin/config')?AdminConfig:location.pathname.startsWith('/admin')?AdminUsers:Account;
 const app=Vue.createApp({render(){return Vue.h(page,{ref:'page'});},mounted(){window.account=this.$refs.page;}});
 app.use(ElementPlus);app.component('RouterLink',{props:['to'],template:'<a><slot /></a>'});
 Object.assign(app.config.globalProperties,{$store:store,$route:route,$router:router,$confirm:ElementPlus.ElMessageBox.confirm,$message:ElementPlus.ElMessage});app.mount('#app');window.store=store;
});`
artifacts['/center.js'] = Buffer.from(script)
artifacts['/center.css'] = Buffer.from(css)
const html = '<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><link rel="stylesheet" href="/element.css"><link rel="stylesheet" href="/center.css"><body><div id="app"></div><script src="/vue.js"></script><script src="/element.js"></script><script src="/axios.js"></script><script src="/center.js"></script></body></html>'
before(async () => {
  server = http.createServer((req,res) => {
    const artifact = artifacts[req.url]
    res.setHeader('Content-Type', artifact ? req.url.endsWith('.css') ? 'text/css' : 'application/javascript' : 'text/html; charset=utf-8')
    res.end(artifact || html)
  })
  await new Promise(resolve => server.listen(0,'127.0.0.1',resolve)); origin = 'http://127.0.0.1:' + server.address().port
  const chrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'
  const executablePath = process.env.CHROME_PATH || (process.platform === 'darwin' && fs.existsSync(chrome) ? chrome : undefined)
  browser = await chromium.launch({headless:true, ...(executablePath ? {executablePath} : {})})
})
after(async () => { if(browser)await browser.close(); if(server)await new Promise(resolve=>server.close(resolve)) })

function fixture() {
  const user = {id:'42',user_name:'friend',display_name:'朋友',contact_email:'friend@example.com',contact_mobile:'',revision:'7',avatar_version:0,avatar_url:'',status:'active',role:'admin',csrf_token:'fixture-csrf',password_policy:{min_length:15},capabilities:{},create_time:'2026-09-17T01:00:00Z'}
  const session = {id:'9223372036854775807',client_name:'Chrome',os_name:'Linux',device_type:'desktop',login_ip:'2001:db8::1',login_source:'web',purpose:'user',authenticated_at:'2026-09-17T01:00:00Z',expire_time:'2026-09-18T01:00:00Z',remaining_seconds:86400,user_agent:'Mozilla/5.0 fixture'}
  return {user,writes:[],requests:[],current:{...session,id:'current'},sessions:[session],maxActiveSessions:5,failSessions:false,avatar:null}
}
async function setup(options = {}) {
  const state = options.state || fixture(), context = options.context || await browser.newContext({viewport:{width:options.width || 1280,height:950}}), page = await context.newPage()
  page.setDefaultTimeout(10000)
  const errors=[];page.on('pageerror',error=>{errors.push(error.message);console.error(error.message)})
  await page.route('**/api/**',async route => {
    const req=route.request(),url=new URL(req.url());state.requests.push(url.pathname)
    let data
    if(url.pathname==='/api/auth/me')data=state.user
    else if(url.pathname==='/api/admin/config/schema')data={namespaces:state.configSchemas}
    else if(url.pathname==='/api/admin/config' && req.method()==='GET')data={items:[state.configRecord]}
    else if(url.pathname==='/api/admin/config/status')data={apply_state:'applied',persisted_generation:state.configRecord.revision,loaded_generation:state.configRecord.revision,namespaces:[{namespace:'account_policy',loaded_revision:state.configRecord.revision,apply_state:'applied'}]}
    else if(url.pathname==='/api/admin/config/account_policy/history')data={items:state.configHistory,has_more:false}
    else if(url.pathname==='/api/admin/config/account_policy/validate') {
      const values=req.postDataJSON().values;state.configValidations.push(values)
      data={revision:state.configRecord.revision,values,changes:[{key:'max_active_sessions',before:state.configRecord.values.max_active_sessions,after:values.max_active_sessions}],effects:['new_operation']}
    }
    else if(url.pathname.endsWith('/sessions')) {
      if(state.failSessions){await route.fulfill({status:503,json:{code:503,message:'合成连接失败'}});return}
      data={current_session:url.pathname.startsWith('/api/admin')?null:state.current,items:state.sessions,total_count:state.sessions.length+1,restricted_count:0,max_active_sessions:state.maxActiveSessions,server_time:'2026-09-17T01:00:00Z',has_more:false}
    } else if(url.pathname==='/api/admin/users')data={items:[{...state.user,active_session_count:11,restricted_session_count:0}],session_warning_threshold:10}
    else if(url.pathname==='/api/admin/users/42')data={user:state.user,applications:[],invitations:[],projects:[]}
    else if(req.method()!=='GET') {
      let body;try{body=req.postDataJSON()}catch(_){body=req.postDataBuffer()}
      state.writes.push({path:url.pathname,headers:req.headers(),body})
      if(url.pathname.endsWith('/profile')) {Object.assign(state.user,body);state.user.revision=String(Number(state.user.revision)+1);data=state.user}
      else if(url.pathname==='/api/account/avatar') {
        if(state.avatarConflict){state.avatarConflict=false;state.user.revision='8';state.user.display_name='';await route.fulfill({status:409,json:{code:409,message:'合成资料版本冲突'}});return}
        state.avatar=body;state.user.avatar_url='/api/account/avatars/42/8';state.user.revision='8';data=state.user
      }
      else if(url.pathname.endsWith('/revoke')){state.sessions=state.sessions.filter(item=>!body.session_ids.includes(item.id));data={revoked_count:1,already_inactive_count:0}}
      else if(url.pathname.endsWith('/reset-profile')) {if(body.reset_display_name)state.user.display_name='';if(body.reset_avatar)state.user.avatar_url='';data={user:state.user}}
      else if(url.pathname==='/api/admin/config/account_policy/rollback') {state.configRecord={...state.configRecord,revision:state.configRecord.revision+1,values:{...state.configHistory[0].values,max_active_sessions:5}};data=state.configRecord}
      else data={}
    } else if(url.pathname.startsWith('/api/account/avatars/')) {await route.fulfill({status:404,body:'removed'});return}
    else data={}
    await route.fulfill({json:{code:0,data}})
  })
  await page.goto(origin+(options.path || '/account'));await page.waitForFunction(()=>!!window.account).catch(error=>{error.message+='; '+errors.join('; ');throw error})
  assert.deepEqual(errors,[])
  return {page,context,state,errors}
}

test('昵称清空只提交昵称，跨标签更新保留已编辑草稿，320px页面没有水平溢出',async()=>{
  const {page,context,state}=await setup({width:320})
  try {
    await page.getByLabel('昵称',{exact:true}).waitFor()
    fs.mkdirSync(visualOutput,{recursive:true})
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
    await page.screenshot({path:path.join(visualOutput,'account-center-mobile-320.png'),fullPage:true})
    await page.getByLabel('昵称',{exact:true}).fill('😀'.repeat(64))
    assert.equal(await page.getByLabel('昵称',{exact:true}).inputValue(),'😀'.repeat(64))
    await page.getByLabel('昵称',{exact:true}).fill('')
    await page.getByRole('button',{name:'保存资料',exact:true}).click()
    await page.waitForFunction(()=>window.account.revision==='8')
    assert.deepEqual(state.writes[0].body,{display_name:''})
    const second=await setup({context,state})
    await second.page.getByLabel('昵称',{exact:true}).fill('尚未保存的草稿')
    await page.getByLabel('昵称',{exact:true}).fill('另一标签更新')
    await page.getByRole('button',{name:'保存资料',exact:true}).click()
    await second.page.getByText('资料已发生变化，草稿已保留',{exact:true}).waitFor()
    assert.equal(await second.page.getByLabel('昵称',{exact:true}).inputValue(),'尚未保存的草稿')
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  }finally{await context.close()}
})

test('真实照片按EXIF方向裁剪成256px PNG，拖动缩放可用，取消不上传',async()=>{
  const {page,context,state}=await setup({width:320})
  try {
    const input=await page.evaluate(async()=>{
      const canvas=document.createElement('canvas');canvas.width=100;canvas.height=50;const draw=canvas.getContext('2d');draw.fillStyle='blue';draw.fillRect(0,0,50,50);draw.fillStyle='red';draw.fillRect(50,0,50,50)
      const blob=await new Promise(resolve=>canvas.toBlob(resolve,'image/jpeg',.95)),jpeg=new Uint8Array(await blob.arrayBuffer())
      const app=new Uint8Array([255,225,0,34,69,120,105,102,0,0,73,73,42,0,8,0,0,0,1,0,18,1,3,0,1,0,0,0,6,0,0,0,0,0,0,0])
      return Array.from(new Uint8Array([...jpeg.slice(0,2),...app,...jpeg.slice(2)]))
    })
    await page.getByRole('button',{name:'更换头像',exact:true}).click()
    await page.getByLabel('选择头像图片').setInputFiles({name:'oriented.jpg',mimeType:'image/jpeg',buffer:Buffer.from(input)})
    const canvas=page.getByRole('img',{name:'头像裁剪预览，可拖动或使用方向键移动'})
    await canvas.waitFor()
    const bounds=await canvas.boundingBox();assert.ok(Math.abs(bounds.width-bounds.height)<0.5)
    assert.ok((await page.getByRole('button',{name:'保存头像',exact:true}).boundingBox()).height>=44)
    await canvas.focus();await page.keyboard.press('ArrowDown')
    await page.getByRole('slider',{name:'头像缩放'}).focus();await page.keyboard.press('ArrowRight')
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
    await page.getByRole('button',{name:'取消',exact:true}).last().click()
    assert.equal(state.writes.length,0)
    await page.getByRole('button',{name:'更换头像',exact:true}).click()
    await page.getByLabel('选择头像图片').setInputFiles({name:'oriented.jpg',mimeType:'image/jpeg',buffer:Buffer.from(input)})
    await canvas.waitFor()
    const colors=await canvas.evaluate(node=>{const c=node.getContext('2d');return [Array.from(c.getImageData(140,50,1,1).data),Array.from(c.getImageData(140,230,1,1).data)]})
    assert.ok(colors[0][2]>200&&colors[0][0]<50);assert.ok(colors[1][0]>200&&colors[1][2]<50)
    await page.getByRole('button',{name:'保存头像',exact:true}).click()
    await page.waitForFunction(()=>window.account.revision==='8')
    assert.ok(state.avatar.includes(Buffer.from('image/png')))
    const index=state.avatar.indexOf(Buffer.from([137,80,78,71,13,10,26,10]));assert.ok(index>=0)
    assert.equal(state.avatar.readUInt32BE(index+16),256);assert.equal(state.avatar.readUInt32BE(index+20),256)
    assert.equal(state.writes[0].headers['if-match'],'7')
  }finally{await context.close()}
})

test('登录管理只退出选中会话，刷新失败保留数量及卡片，初始改密仍限制入口',async()=>{
  const {page,context,state}=await setup({path:'/account/sessions',width:320})
  try {
    await page.getByText('有效登录会话 2 个',{exact:false}).waitFor()
    const policy = page.getByText('本网站每个账号的有效登录上限为',{exact:false})
    assert.match(await policy.textContent(), /上限为 5 个会话，当前 2 \/ 5/)
    assert.match(await policy.textContent(), /达到上限时会拒绝新的登录；已存在的超额会话保留，可主动退出或等待过期/)
    await page.getByLabel('选择 Chrome · Linux · 桌面').check()
    await page.getByRole('button',{name:'退出选中 1',exact:true}).click()
    await page.getByRole('button',{name:'确认退出选中',exact:true}).click()
    await page.getByText('暂无其他有效登录',{exact:true}).waitFor()
    assert.deepEqual(state.writes[0].body,{session_ids:['9223372036854775807']})
    state.failSessions=true
    await page.getByRole('button',{name:'刷新',exact:true}).click()
    await page.getByText('刷新失败，保留上次确认的数据',{exact:true}).waitFor()
    assert.ok(await page.getByText('有效登录会话 1 个',{exact:false}).count())
    assert.match(await policy.textContent(), /上限为 5 个会话，当前 1 \/ 5/)
    state.user.must_change_password=true
    await page.evaluate(()=>window.store.dispatch('refreshSession'))
    assert.match(await page.getByRole('tab',{name:'登录管理'}).getAttribute('class'),/is-disabled/)
    assert.equal(await page.getByRole('button',{name:'更换头像',exact:true}).isVisible(),false)
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  }finally{await context.close()}
})

test('头像版本冲突可查看最新资料后保留裁剪和昵称草稿继续保存',async()=>{
  const {page,context,state}=await setup({width:390})
  try {
    await page.getByLabel('昵称',{exact:true}).fill('未保存昵称')
    const input=await page.evaluate(()=>{const canvas=document.createElement('canvas');canvas.width=canvas.height=64;canvas.getContext('2d').fillRect(0,0,64,64);return canvas.toDataURL().split(',')[1]})
    await page.getByRole('button',{name:'更换头像',exact:true}).click()
    await page.getByLabel('选择头像图片').setInputFiles({name:'avatar.png',mimeType:'image/png',buffer:Buffer.from(input,'base64')})
    const preview=page.getByRole('img',{name:'头像裁剪预览，可拖动或使用方向键移动'})
    await preview.waitFor()
    state.avatarConflict=true
    await page.getByRole('button',{name:'保存头像',exact:true}).click()
    await page.getByRole('dialog',{name:'最新个人资料',exact:true}).waitFor()
    await page.getByRole('button',{name:'保留草稿继续编辑',exact:true}).click()
    await preview.waitFor()
    assert.equal(await page.getByLabel('昵称',{exact:true}).inputValue(),'未保存昵称')
    await page.getByRole('button',{name:'保存头像',exact:true}).click()
    await page.waitForFunction(()=>window.account.user.avatar_url)
    assert.equal(state.writes.length,2)
    assert.equal(state.writes[1].headers['if-match'],'8')
    assert.equal(await page.getByLabel('昵称',{exact:true}).inputValue(),'未保存昵称')
  }finally{await context.close()}
})

test('管理员数量10仅提示，资料重置可勾选头像并携带密码、原因和revision',async()=>{
  const {page,context,state}=await setup({path:'/admin/users'})
  try {
    await page.getByText('数量偏多，建议核查',{exact:true}).waitFor()
    await page.evaluate(()=>window.account.beginAction(window.account.items[0],'reset-profile'))
    await page.getByText('头像',{exact:true}).click()
    await page.getByLabel('操作原因').fill('合成资料处置')
    await page.getByLabel('你的当前密码').fill('fixture-current-password')
    await page.getByRole('button',{name:'确认重置昵称 / 头像',exact:true}).click()
    await page.waitForFunction(()=>!window.account.actionVisible)
    assert.deepEqual(state.writes[0].body,{reset_avatar:true,reset_display_name:false,reason:'合成资料处置',current_password:'fixture-current-password'})
    assert.equal(state.writes[0].headers['if-match'],'7')
  }finally{await context.close()}
})

test('站点设置可回滚旧四字段账号策略，校验补5且历史原始记录不变',async()=>{
  const state=fixture(),legacy={session_ttl_seconds:604800,temporary_password_ttl_days:7,min_password_length:15,bcrypt_cost:12}
  const defaults={...legacy,max_active_sessions:5}
  const label='每个账号同时有效的网站登录上限'
  state.configSchemas=[{namespace:'account_policy',label:'账号策略',fields:Object.entries(defaults).map(([key,default_value])=>({key,label:key==='max_active_sessions'?label:key,type:'integer',default_value,minimum:1,maximum:key==='max_active_sessions'?100:2592000,effect:'new_operation'}))}]
  state.configRecord={namespace:'account_policy',revision:2,values:{...legacy,max_active_sessions:3},apply_state:'applied',loaded_revision:2}
  state.configHistory=[{namespace:'account_policy',revision:1,values:legacy,reason:'合成旧配置',actor_user_id:'42'}]
  state.configValidations=[]
  const {page,context}=await setup({path:'/admin/config',state,width:390})
  try {
    const limit=page.locator('.config-field').filter({hasText:label}).getByRole('spinbutton')
    assert.equal(await limit.inputValue(),'3')
    assert.ok(await page.getByText('降低上限不会主动退出已有会话',{exact:false}).isVisible())
    await page.getByRole('button',{name:'版本历史',exact:true}).click()
    const history=page.getByRole('dialog',{name:'账号策略 · 版本历史',exact:true})
    await history.locator('.el-table__expand-icon').click()
    assert.equal(await history.getByText(label,{exact:true}).count(),0)
    await history.getByRole('button',{name:'回滚',exact:true}).click()
    const confirmation=page.getByRole('dialog',{name:'回滚配置',exact:true})
    await confirmation.waitFor()
    assert.equal(state.configValidations[0].max_active_sessions,5)
    assert.equal(Object.hasOwn(legacy,'max_active_sessions'),false)
    await confirmation.getByLabel('操作原因').fill('合成旧策略回滚')
    await confirmation.getByLabel('你的当前密码').fill('fixture-current-password')
    await confirmation.getByRole('button',{name:'确认回滚配置',exact:true}).click()
    await page.waitForFunction(()=>window.account.current.revision===3)
    assert.equal(await limit.inputValue(),'5')
    assert.equal(state.writes[0].path,'/api/admin/config/account_policy/rollback')
    assert.equal(state.writes[0].body.target_revision,1)
    assert.equal(state.writes[0].headers['if-match'],'2')
    assert.equal(Object.hasOwn(state.writes[0].body,'values'),false)
    assert.equal(Object.hasOwn(legacy,'max_active_sessions'),false)
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false)
  }finally{await context.close()}
})
