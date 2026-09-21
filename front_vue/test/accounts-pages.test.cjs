const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const test = require('node:test')
const vm = require('node:vm')

function component(name, api = {}) {
    const source = fs.readFileSync(path.join(__dirname, `../src/views/${name}.vue`), 'utf8')
    const script = source.match(/<script>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '').replace('export default', 'module.exports =')
    const box = {module: {exports: {}}, MyHeader: {}, UserIdentity: {}, AvatarCropper: {}, LoginSessions: {}, AdminConfirm: {}, Key: {}, Plus: {}, Refresh: {}, bookFallback: 'fixture.svg', alert() {}, console, setInterval, clearInterval, URLSearchParams,
        require: name => name === '@/api/accounts.cjs' ? {accountsApi: api} : require(path.join(__dirname, '../src/', name.slice(2))),
    }
    vm.runInNewContext(script, box, {filename: `${name}.vue`})
    const definition = box.module.exports
    const routes = [], commits = []
    const view = {$route: {path: '/account', query: {}}, $store: {state: {userInfo: {id: '9223372036854775807', status: 'active', revision: 7}}, commit: (...args) => commits.push(args), dispatch: async () => {}}, $router: {replace: route => routes.push(route)}, $message: {success() {}, warning() {}}, $confirm: async () => {}}
    Object.assign(view, definition.data.call(view))
    for (const [key, method] of Object.entries(definition.methods || {})) view[key] = method.bind(view)
    for (const [key, getter] of Object.entries(definition.computed || {})) Object.defineProperty(view, key, {get: () => getter.call(view)})
    return {view, routes, commits}
}

test('profile conflict preserves edited fields and does not overwrite the user or revision', async () => {
    const conflict = Object.assign(new Error('资料版本冲突'), {status: 409})
    const {view, commits} = component('Account', {profile: async () => {throw conflict}})
    view.profile = {display_name: '尚未保存的资料', contact_email: '', contact_mobile: ''}; view.revision = 7
    await view.saveProfile()
    assert.equal(view.profile.display_name, '尚未保存的资料')
    assert.equal(view.revision, 7)
    assert.equal(view.conflict, true)
    assert.deepEqual(commits, [])
})

test('password change clears secrets and identity only after the server confirms success', async () => {
    const {view, routes, commits} = component('Account', {changePassword: async () => ({})})
    view.password = {current_password: 'old', new_password: 'a sufficiently long password', confirm_password: 'a sufficiently long password'}
    await view.changePassword()
    assert.equal(view.password.current_password, '')
    assert.equal(view.password.new_password, '')
    assert.deepEqual(commits, [['REMOVE_INFO']])
    assert.equal(routes[0].path, '/login')
    assert.equal(routes[0].query.changed, '1')
})

test('password change rejection keeps the edit page and displays the server reason', async () => {
    const {view, routes, commits} = component('Account', {changePassword: async () => {throw new Error('当前密码不正确')}})
    view.password = {current_password: 'old', new_password: 'a sufficiently long password', confirm_password: 'a sufficiently long password'}
    await view.changePassword()
    assert.equal(view.error, '当前密码不正确')
    assert.deepEqual(routes, [])
    assert.deepEqual(commits, [])
})

test('invitation validation does not apply an old response to an edited code', async () => {
    let finish
    const {view} = component('Register', {validateInvitation: () => new Promise(resolve => {finish = resolve})})
    view.form.invitation_code = 'first'
    const pending = view.validateCode()
    view.form.invitation_code = 'second'
    finish({valid: true, expires_at: '2026-09-20T12:00:00Z'})
    await pending
    assert.equal(view.invitation, null)
})

test('a config publish conflict retains the draft and revision for explicit review', async () => {
    const conflict = Object.assign(new Error('版本冲突'), {status: 409})
    const {view} = component('AdminConfig', {publishConfig: async () => {throw conflict}})
    view.namespace = 'site'; view.records = [{namespace: 'site', revision: 7, values: {title: '当前标题'}}]
    view.draft = {title: '草稿标题'}; view.validation = {values: {...view.draft}}; view.pendingRequest = {request_id: 'id', values: {...view.draft}, reason: '调整名称'}
    await view.publish({current_password: 'secret', reason: '调整名称'})
    assert.equal(view.current.revision, 7)
    assert.equal(view.draft.title, '草稿标题')
    assert.equal(view.conflict, true)
    assert.equal(view.pendingRequest, null)
})

test('legacy account policy rollback fills additive session and password limits without modifying history', async () => {
    const legacy = {session_ttl_seconds: 604800, temporary_password_ttl_days: 7, min_password_length: 15, bcrypt_cost: 12}
    const fields = Object.entries({...legacy, max_active_sessions: 5, min_share_password_length: 8, share_code_length: 6}).map(([key, default_value]) => ({key, label: key, type: 'integer', default_value, minimum: 1, maximum: key === 'max_active_sessions' ? 100 : 2592000}))
    const requests = []
    const {view} = component('AdminConfig', {
        validateConfig: async (...args) => {requests.push(['validate', ...args]); return {revision: 7, values: args[1]}},
        rollbackConfig: async (...args) => {requests.push(['rollback', ...args]); return {namespace: 'account_policy', revision: 8, values: {...legacy, max_active_sessions: 5, min_share_password_length: 8, share_code_length: 6}}},
        configStatus: async () => ({apply_state: 'applied'}),
    })
    view.namespace = 'account_policy'; view.schemas = [{namespace: 'account_policy', fields}]
    view.records = [{namespace: 'account_policy', revision: 7, values: {...legacy, max_active_sessions: 3}}]
    view.resetDraft()
    const record = {revision: 1, values: legacy}
    await view.prepareRollback(record)
    assert.equal(view.historyError, '')
    assert.equal(requests[0][2].max_active_sessions, 5)
    assert.equal(requests[0][2].min_share_password_length, 8)
    assert.equal(requests[0][2].share_code_length, 6)
    assert.equal(Object.hasOwn(record.values, 'share_code_length'), false)
    assert.equal(Object.hasOwn(record.values, 'max_active_sessions'), false)
    assert.equal(view.draft.max_active_sessions, 3)
    assert.equal(view.confirmVisible, true)
    await view.publish({current_password: 'fixture-password', reason: '合成旧版本回滚'})
    assert.equal(requests[1][0], 'rollback')
    assert.equal(requests[1][2], 7)
    assert.equal(requests[1][3].target_revision, 1)
    assert.equal(Object.hasOwn(requests[1][3], 'values'), false)
    assert.equal(view.current.revision, 8)
    assert.equal(view.draft.max_active_sessions, 5)
    assert.equal(Object.hasOwn(record.values, 'max_active_sessions'), false)
})

test('legacy rollback compatibility keeps other missing fields and explicit invalid limits rejected', async () => {
    const defaults = {session_ttl_seconds: 604800, temporary_password_ttl_days: 7, min_password_length: 15, bcrypt_cost: 12, max_active_sessions: 5}
    const fields = Object.entries(defaults).map(([key, default_value]) => ({key, label: key, type: 'integer', default_value, minimum: 1, maximum: key === 'max_active_sessions' ? 100 : 2592000}))
    for (const change of [values => {delete values.min_password_length}, values => {values.max_active_sessions = null}, values => {values.max_active_sessions = 101}]) {
        const requests = []
        const {view} = component('AdminConfig', {validateConfig: async (...args) => {requests.push(args); return {revision: 7, values: args[1]}}})
        view.namespace = 'account_policy'; view.schemas = [{namespace: 'account_policy', fields}]
        view.records = [{namespace: 'account_policy', revision: 7, values: defaults}]
        const candidate = {...defaults}; change(candidate)
        await view.prepareRollback({revision: 1, values: candidate})
        assert.ok(view.historyError)
        assert.equal(view.confirmVisible, false)
        assert.deepEqual(requests, [])
    }
})

test('administrator preview offers only ready release files, excluding cleanup in progress', () => {
    const {view} = component('AdminProjects')
    assert.equal(view.canPreview({status: 'ready'}), true)
    assert.equal(view.canPreview({status: 'deleting'}), false)
    assert.equal(view.canPreview({status: 'ready', purged_at: '2026-09-13T10:00:00Z'}), false)
})

test('application creation uses the current server TTL policy rather than a fixed 90-day value', () => {
    const {view} = component('Applications')
    view.$store.state.userInfo.application_policy = {default_credential_ttl_days: 100, max_credential_ttl_days: 1000, max_applications_per_user: 5}
    view.openCreate()
    assert.equal(view.form.expiresInDays, 100)
    assert.equal(view.maxCredentialTTLDays, 1000)
})

test('an empty real library response clears the table without dereferencing a null list', () => {
    const {view} = component('BookList')
    view.tableData = [{title: 'stale row'}]
    assert.doesNotThrow(() => view.updateTable(null))
    assert.equal(view.tableData.length, 0)
    view.updateTable([{title: 'A book', isbn: '123', quantity: 2}])
    assert.equal(view.tableData[0].title, 'A book')
    assert.equal(view.tableData[0].number, 2)
})
