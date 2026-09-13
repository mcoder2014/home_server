const assert = require('node:assert/strict')
const test = require('node:test')
const {createAccountsApi} = require('../src/api/accounts.cjs')

function transportWith(body = {code: 0, data: {id: '9223372036854775807'}}) {
    const calls = []
    return {calls, async request(config) { calls.push(config); return {status: 200, data: body} }}
}

test('login uses a credentialed cookie request and preserves int64 string IDs', async () => {
    const transport = transportWith()
    const api = createAccountsApi(transport, () => '')
    const me = await api.login({user_name: 'friend', password: 'a sufficiently long password'})
    assert.equal(me.id, '9223372036854775807')
    assert.equal(transport.calls[0].url, '/api/auth/login')
    assert.equal(transport.calls[0].withCredentials, true)
    assert.equal(transport.calls[0].headers.passport, undefined)
    assert.equal(transport.calls[0].headers.Authorization, undefined)
})

test('profile and admin writes include CSRF and the exact revision; conflict stays an error', async () => {
    const transport = transportWith()
    const api = createAccountsApi(transport, () => 'csrf-value')
    await api.profile('101', {display_name: '朋友'})
    await api.userAction('9223372036854775807', 'role', '102', {role: 'admin', reason: '指定管理员', current_password: 'secret'})
    assert.equal(transport.calls[0].headers['X-CSRF-Token'], 'csrf-value')
    assert.equal(transport.calls[0].headers['If-Match'], '101')
    assert.equal(transport.calls[1].url, '/api/admin/users/9223372036854775807/role')
    assert.equal(transport.calls[1].method, 'put')
    const failed = createAccountsApi({async request() { const e = new Error(); e.response = {status: 409, data: {code: 40901, message: '版本冲突'}}; throw e }})
    await assert.rejects(() => failed.profile('101', {display_name: '仍保留的草稿'}), e => e.status === 409 && e.message === '版本冲突')
})

test('config validation, publishing and rollback send version and idempotency boundaries', async () => {
    const transport = transportWith()
    const api = createAccountsApi(transport, () => 'csrf')
    await api.validateConfig('registration', {enabled: false})
    await api.publishConfig('registration', '7', {request_id: 'request-1', values: {enabled: false}, reason: '维护', current_password: 'secret'})
    await api.rollbackConfig('registration', '8', {request_id: 'request-2', target_revision: 6, reason: '回滚', current_password: 'secret'})
    assert.equal(transport.calls[0].url, '/api/admin/config/registration/validate')
    assert.equal(transport.calls[1].headers['If-Match'], '7')
    assert.equal(transport.calls[1].data.request_id, 'request-1')
    assert.equal(transport.calls[2].data.target_revision, 6)
})

test('administrator invitation revocation carries owner revision and confirmation', async () => {
    const transport = transportWith()
    const api = createAccountsApi(transport, () => 'csrf')
    await api.revokeUserInvitation('10', '11', '12', {reason: '风险链接', current_password: 'secret'})
    assert.equal(transport.calls[0].url, '/api/admin/users/10/invitations/11/revoke')
    assert.equal(transport.calls[0].headers['If-Match'], '12')
})
