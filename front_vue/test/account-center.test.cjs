const assert = require('node:assert/strict')
const test = require('node:test')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const behavior = require('../src/utils/accounts_behavior.cjs')
const {createAccountsApi} = require('../src/api/accounts.cjs')

test('login redirect restores the sessions page after authentication', () => {
    const {normalizeInternalRedirect} = require('../src/utils/web_projects_navigation.cjs')
    assert.equal(normalizeInternalRedirect('/account/sessions'), '/account/sessions')
})

test('HTML proxy errors for uploads and rate limits produce actionable Chinese messages', async () => {
    for (const [status, message] of [[413,/过大/],[429,/稍后/]]) {
        const api = createAccountsApi({async request() {throw Object.assign(new Error('HTTP failure'),{response:{status,data:'<html>proxy error</html>'}})}})
        await assert.rejects(api.uploadAvatar('7',new FormData()),error=>error.status===status&&message.test(error.message))
    }
})

function component(file, api = {}, props = {}, globals = {}) {
    const source = fs.readFileSync(path.join(__dirname, '../src/', file), 'utf8')
    const script = source.match(/<script>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '').replace('export default', 'module.exports =')
    const box = {module: {exports: {}}, MyHeader: {}, UserIdentity: {}, AvatarCropper: {}, LoginSessions: {}, SessionDetails: {}, AdminConfirm: {}, TextEncoder, Date, Intl, URL, FormData, setInterval, clearInterval,
        require: name => name === '@/api/accounts.cjs' ? {accountsApi: api} : require(path.join(__dirname, '../src/', name.slice(2))),
        ...globals,
    }
    vm.runInNewContext(script, box, {filename: file})
    const definition = box.module.exports, commits = [], confirmations = []
    const view = {$route: {path: '/account', query: {}}, $store: {state: {userInfo: {id: '42', user_name: 'friend', status: 'active', revision: '7'}}, commit: (...args) => commits.push(args), dispatch: async () => {}}, $router: {replace() {}}, $message: {success() {}, warning() {}}, $confirm: async (...args) => confirmations.push(args), ...props}
    Object.assign(view, definition.data.call(view))
    for (const [key, method] of Object.entries(definition.methods || {})) view[key] = method.bind(view)
    for (const [key, getter] of Object.entries(definition.computed || {})) Object.defineProperty(view, key, {get: () => getter.call(view)})
    return {view, definition, commits, confirmations}
}

test('empty nickname uses the username and profile patches contain only changed fields', () => {
    const saved = {display_name: '朋友', contact_email: 'friend@example.com', contact_mobile: ''}
    assert.equal(behavior.profileError({...saved, display_name: '   '}), '')
    assert.deepEqual(behavior.profilePayload({...saved, display_name: '   ', role: 'admin'}, saved), {display_name: ''})
    assert.deepEqual(behavior.profilePayload(saved, saved), {})
    assert.match(behavior.profileError({...saved, display_name: '界'.repeat(65)}), /64/)
})

test('identity placeholders preserve a Unicode grapheme and accept only the account avatar route', () => {
    assert.equal(typeof behavior.userIdentity, 'function')
    assert.equal(behavior.userIdentity({display_name: ' 👩🏽‍💻 开发者 ', user_name: 'friend'}).initial, '👩🏽‍💻')
    assert.equal(behavior.userIdentity({display_name: '', user_name: 'friend'}).name, 'friend')
    assert.equal(behavior.userIdentity({display_name: '\u200b\u0000', user_name: ''}).initial, '')
    assert.equal(behavior.userIdentity({avatar_url: 'https://tracker.example/avatar.png'}).avatar, '')
    assert.equal(behavior.userIdentity({avatar_url: '//tracker.example/avatar.png'}).avatar, '')
    assert.equal(behavior.userIdentity({avatar_url: '/api/account/avatars/42/8'}).avatar, '/api/account/avatars/42/8')
})

test('new identity and independent avatar saves retain only locally edited profile fields', () => {
    const {view} = component('views/Account.vue')
    view.savedProfile = {display_name: '旧昵称', contact_email: 'old@example.com', contact_mobile: ''}
    view.profile = {...view.savedProfile, display_name: '草稿昵称'}
    const latest = {id: '42', user_name: 'friend', revision: '8', display_name: '旧昵称', contact_email: 'new@example.com', contact_mobile: '', avatar_url: '/api/account/avatars/42/8'}
    assert.equal(typeof view.acceptAvatar, 'function')
    view.acceptAvatar(latest)
    assert.equal(view.profile.display_name, '草稿昵称')
    assert.equal(view.profile.contact_email, 'new@example.com')
    assert.equal(view.revision, '8')
    assert.equal(view.dirty, true)
    const remote = {...latest, revision: '9', display_name: '管理员重置'}
    view.applyIdentity(remote)
    assert.equal(view.profile.display_name, '草稿昵称')
    assert.equal(view.conflict, true)
    assert.equal(view.latest.display_name, '管理员重置')
})

test('profile saving sends a field patch and resetting cancels to the latest identity', async () => {
    const calls = []
    const {view} = component('views/Account.vue', {profile: async (revision, patch) => {calls.push({revision, patch}); return {id: '42', user_name: 'friend', revision: '8', display_name: '', contact_email: 'friend@example.com', contact_mobile: ''}}})
    view.savedProfile = {display_name: '朋友', contact_email: 'friend@example.com', contact_mobile: ''}
    view.profile = {...view.savedProfile, display_name: ''}; view.revision = '7'
    await view.saveProfile()
    assert.deepEqual(JSON.parse(JSON.stringify(calls)), [{revision: '7', patch: {display_name: ''}}])
})

test('session and avatar requests preserve opaque cursors, exact IDs and CSRF revisions', async () => {
    const calls = [], api = createAccountsApi({async request(config) {calls.push(config); return {status: 200, data: {code: 0, data: {}}}}}, () => 'csrf')
    assert.equal(typeof api.sessions, 'function')
    await api.sessions({cursor: 'opaque+/==', limit: 20})
    await api.userSessions('9223372036854775807', {cursor: 'next', limit: 20})
    await api.revokeSessions(['9223372036854775806'])
    await api.revokeOtherSessions()
    const form = new FormData(); form.append('file', new Blob(['png'], {type: 'image/png'}), 'avatar.png')
    await api.uploadAvatar('11', form)
    await api.removeAvatar('12')
    await api.userAction('42', 'reset-profile', '13', {reset_display_name: true, reset_avatar: false, reason: '违规', current_password: 'secret'})
    assert.equal(calls[0].params.cursor, 'opaque+/==')
    assert.equal(calls[1].url, '/api/admin/users/9223372036854775807/sessions')
    assert.deepEqual(calls[2].data, {session_ids: ['9223372036854775806']})
    assert.equal(calls[3].data, undefined)
    assert.equal(calls[4].data, form)
    assert.equal(calls[4].headers['Content-Type'], undefined)
    assert.equal(calls[4].headers['X-CSRF-Token'], 'csrf')
    assert.equal(calls[4].headers['If-Match'], '11')
    assert.equal(calls[5].method, 'delete')
    assert.equal(calls[6].headers['If-Match'], '13')
})

test('session selection excludes the current session and counts down from server remaining time', () => {
    assert.equal(typeof behavior.selectedSessionIDs, 'function')
    assert.deepEqual(behavior.selectedSessionIDs(['current', 'other', 'other', 'unknown'], [{id: 'other'}], {id: 'current'}), ['other'])
    assert.equal(behavior.sessionRemaining({remaining_seconds: 120}, 1000, 31000), 90)
    assert.equal(behavior.sessionRemaining({remaining_seconds: 20}, 1000, 31000), 0)
    assert.equal(behavior.sessionRemaining({remaining_seconds: 20}, 1000, 0), 20)
})

test('session query failures retain the last confirmed data and pagination cursor', async () => {
    const {view} = component('components/LoginSessions.vue', {sessions: async () => {throw new Error('连接失败')}}, {userId: '', administrator: false})
    view.items = [{id: 'other'}]; view.cursor = 'opaque'; view.loaded = true; view.lastUpdated = '2026-09-17T09:00:00Z'; view.total = 2
    await view.load(true)
    assert.equal(view.items[0].id, 'other')
    assert.equal(view.cursor, 'opaque')
    assert.equal(view.total, 2)
    assert.match(view.error, /连接失败/)
})

test('session limits use the website policy returned for both user and administrator pages', async () => {
    for (const administrator of [false, true]) {
        const page = {current_session: null, items: [], total_count: 2, restricted_count: 0, max_active_sessions: 3, server_time: '2026-09-18T09:00:00Z'}
        const api = {sessions: async () => page, userSessions: async () => page}
        const {view} = component('components/LoginSessions.vue', api, {administrator, userId: administrator ? 'other-user' : ''})
        await view.load(true)
        assert.equal(view.maxActiveSessions, 3)
        assert.equal(view.total, 2)
        page.max_active_sessions = 100
        await view.load(true)
        assert.equal(view.maxActiveSessions, 100)
    }
})

test('a failed refresh keeps the last confirmed website session limit', async () => {
    let fail = false
    const {view} = component('components/LoginSessions.vue', {sessions: async () => {
        if (fail) throw new Error('暂时无法连接')
        return {current_session: null, items: [], total_count: 2, max_active_sessions: 5, server_time: '2026-09-18T09:00:00Z'}
    }}, {administrator: false, userId: ''})
    await view.load(true)
    fail = true
    await view.load(true)
    assert.equal(view.maxActiveSessions, 5)
    assert.equal(view.total, 2)
    assert.match(view.error, /暂时无法连接/)
})

test('an older or malformed session response never invents a website session limit', async () => {
    for (const limit of [undefined, null, 0, 101, '5']) {
        const {view} = component('components/LoginSessions.vue', {sessions: async () => ({current_session: null, items: [], total_count: 0, max_active_sessions: limit})}, {administrator: false, userId: ''})
        await view.load(true)
        assert.equal(view.maxActiveSessions, null)
    }
})

test('loading a following session page never extends the earlier page countdown', async () => {
    let now = 1000
    const queries = []
    const {view} = component('components/LoginSessions.vue', {sessions: async query => {
        queries.push(query)
        return {current_session:{id:'current',remaining_seconds:query.cursor ? 270 : 300},items:query.cursor ? [{id:'second',remaining_seconds:180}] : [{id:'first',remaining_seconds:120}],total_count:3,restricted_count:0,server_time:'2026-09-17T09:00:00Z',has_more:!query.cursor,next_cursor:query.cursor ? '' : 'opaque+/=='}
    }}, {administrator:false,userId:''}, {Date:{now:()=>now}})
    await view.load(true)
    now = 31000
    await view.load(false)
    assert.equal(queries[1].cursor,'opaque+/==')
    assert.equal(queries[1].limit,20)
    assert.equal(view.items.length,2)
    assert.equal(view.remaining(view.items[0]),90)
    assert.equal(view.remaining(view.items[1]),180)
    assert.equal(view.remaining(view.current),270)
})

test('an expiry tick refreshes once instead of repeatedly polling an overdue stale card', () => {
    const {view} = component('components/LoginSessions.vue',{}, {administrator:false,userId:''}, {Date:{now:()=>31000}})
    view.current={id:'current',remaining_seconds:20};view.receivedAt=1000;view.loaded=true
    let refreshes=0;view.load=async()=>{refreshes++}
    view.tick();view.tick()
    assert.equal(refreshes,1)
    assert.equal(view.remaining(view.current),0)
})

test('revoke-other confirmation explicitly covers all pages and selected revoke protects current', async () => {
    const calls = []
    const {view, confirmations} = component('components/LoginSessions.vue', {revokeOtherSessions: async () => calls.push('all-other'), revokeSessions: async ids => calls.push(ids), sessions: async () => ({current_session: {id: 'current'}, items: [], total_count: 1, restricted_count: 0, server_time: '2026-09-17T09:00:00Z'})}, {administrator: false, userId: ''})
    view.current = {id: 'current'}; view.items = [{id: 'other', client_name: 'Chrome'}]; view.selected = ['current', 'other']; view.total = 30
    await view.revokeSelected()
    assert.deepEqual(JSON.parse(JSON.stringify(calls[0])), ['other'])
    view.total = 30
    await view.revokeOthers()
    assert.match(confirmations[1][0], /下一页/)
    assert.equal(calls[1], 'all-other')
})

test('administrator profile reset requires at least one field and preserves confirmation revision', async () => {
    const calls = [], {view} = component('views/AdminUsers.vue', {userAction: async (...args) => calls.push(args), users: async () => ({items: []})})
    view.beginAction({id: '43', revision: '17', user_name: 'other', display_name: '其他'}, 'reset-profile')
    view.actionForm.reset_display_name = false; view.actionForm.reset_avatar = false
    await view.executeAction({reason: '违规', current_password: 'secret'})
    assert.equal(calls.length, 0)
    assert.match(view.actionError, /至少/)
    view.actionForm.reset_avatar = true
    await view.executeAction({reason: '违规', current_password: 'secret'})
    assert.equal(calls[0][2], '17')
    assert.equal(calls[0][3].reset_avatar, true)
    assert.equal(calls[0][3].reset_display_name, false)
})

test('avatar input validation happens before decoding and rejects animation and pixel bombs', () => {
    assert.equal(typeof behavior.avatarFileError, 'function')
    assert.match(behavior.avatarFileError({type: 'image/svg+xml', size: 100}), /JPEG|PNG/)
    assert.match(behavior.avatarFileError({type: 'image/png', size: 2097153}), /2 MiB/)
    assert.equal(behavior.avatarFileError({type: 'image/jpeg', size: 2097152}), '')
    const png = Buffer.alloc(33); Buffer.from([137,80,78,71,13,10,26,10]).copy(png); png.writeUInt32BE(13,8); png.write('IHDR',12); png.writeUInt32BE(4097,16); png.writeUInt32BE(4096,20)
    assert.throws(() => behavior.avatarMetadata(png), /4096/)
    png.writeUInt32BE(4096,16)
    assert.throws(() => behavior.avatarMetadata(png), /1600/)
    png.writeUInt32BE(4000,16); png.writeUInt32BE(4000,20)
    assert.equal(behavior.avatarMetadata(png).width, 4000)
    const animation = Buffer.alloc(12); animation.write('acTL',4)
    assert.throws(() => behavior.avatarMetadata(Buffer.concat([png,animation])), /动画/)
})

test('avatar crop stays inside the decoded photo at every zoom and drag edge', () => {
    assert.equal(typeof behavior.avatarCrop, 'function')
    const crop = behavior.avatarCrop(1200, 800, 1, 900, -900, 280)
    assert.deepEqual(crop, {x: 0, y: 0, size: 800, offsetX: 70, offsetY: 0, scale: 0.35})
    const zoom = behavior.avatarCrop(1200, 800, 2, -900, 900, 280)
    assert.equal(zoom.size, 400)
    assert.equal(zoom.x + zoom.size, 1200)
    assert.equal(zoom.y, 0)
})

test('cross-tab updates broadcast an event only and visibility triggers a fresh me request', () => {
    const file = path.join(__dirname, '../src/utils/account_updates.cjs')
    assert.equal(fs.existsSync(file), true)
    const {startAccountUpdates} = require(file), listeners = {}, sent = [], updates = []
    class Channel {constructor() {this.listeners = {}} addEventListener(type, fn) {this.listeners[type] = fn} removeEventListener() {} postMessage(value) {sent.push(value)} close() {}}
    const host = {BroadcastChannel: Channel, addEventListener(type, fn) {listeners[type] = fn}, removeEventListener() {}, document: {visibilityState: 'visible', addEventListener(type, fn) {listeners[type] = fn}, removeEventListener() {}}}
    const connection = startAccountUpdates(host, () => updates.push('me'))
    connection.publish({csrf_token: 'secret', token: 'do-not-broadcast'})
    assert.deepEqual(sent, [{type: 'profile-updated'}])
    listeners.visibilitychange()
    assert.deepEqual(updates, ['me'])
    connection.dispose()
})

test('JPEG metadata reads EXIF orientation and dimensions before decoding the original image', () => {
    const exif = Buffer.alloc(32); exif.write('Exif\u0000\u0000'); exif.write('II',6); exif.writeUInt16LE(42,8); exif.writeUInt32LE(8,10); exif.writeUInt16LE(1,14); exif.writeUInt16LE(0x112,16); exif.writeUInt16LE(3,18); exif.writeUInt32LE(1,20); exif.writeUInt16LE(6,24)
    const app = Buffer.alloc(4); app[0] = 255; app[1] = 225; app.writeUInt16BE(exif.length + 2,2)
    const jpeg = Buffer.concat([Buffer.from([255,216]),app,exif,Buffer.from([255,192,0,8,8,0,100,0,200,1,255,217])])
    assert.equal(behavior.avatarMetadata(jpeg).orientation, 6)
    assert.equal(behavior.avatarMetadata(jpeg).width, 200)
    assert.equal(behavior.avatarMetadata(jpeg).height, 100)
})
