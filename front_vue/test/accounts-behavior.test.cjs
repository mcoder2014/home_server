const assert = require('node:assert/strict')
const test = require('node:test')
const behavior = require('../src/utils/accounts_behavior.cjs')

test('session route decisions require live identity and an explicit capability', () => {
    const admin = {path: '/admin/users', fullPath: '/admin/users', meta: {requireAuth: true, admin: true}}
    assert.equal(behavior.routeDecision(admin, null).path, '/login')
    assert.equal(behavior.routeDecision(admin, {id: '9223372036854775807', role: 'user', status: 'active'}).path, '/forbidden')
    assert.equal(behavior.routeDecision(admin, {id: '1', role: 'admin', status: 'active'}), null)
    assert.equal(behavior.routeDecision(admin, {id: '1', role: 'admin', status: 'active', must_change_password: true}).path, '/account/security')
    const book = {path: '/book/list', meta: {requireAuth: true, library: true}}
    assert.equal(behavior.routeDecision(book, {status: 'active', role: 'admin', library_enabled: false}).path, '/forbidden')
    assert.equal(behavior.routeDecision(book, {status: 'active', library_enabled: true}), null)
})

test('password validation accepts spaces, counts UTF-8 bytes, and rejects mismatched confirmation', () => {
    assert.equal(behavior.passwordError('a sufficiently long password', 'a sufficiently long password'), '')
    assert.match(behavior.passwordError('short', 'short'), /15/)
    assert.match(behavior.passwordError('界'.repeat(25), '界'.repeat(25)), /72/)
    assert.match(behavior.passwordError('a sufficiently long password', 'other'), /一致/)
})

test('self profile submission strips authority fields and preserves string identifiers', () => {
    assert.deepEqual(behavior.profilePayload({id: '9223372036854775807', display_name: '朋友', contact_email: 'friend@example.com', contact_mobile: '', role: 'admin', library_enabled: true}), {display_name: '朋友', contact_email: 'friend@example.com', contact_mobile: ''})
    assert.equal(behavior.profileError({display_name: '', contact_email: '', contact_mobile: ''}), '')
    assert.match(behavior.profileError({display_name: '朋友', contact_email: 'broken', contact_mobile: ''}), /邮箱/)
})

test('moderation blocks owner publishing even if the project is enabled', () => {
    const {canPublishRelease} = require('../src/utils/web_projects_behavior.cjs')
    assert.equal(canPublishRelease({status: 'enabled', moderation_status: 'blocked', current_release_id: '1'}, {id: '2', status: 'ready'}), false)
})

test('config drafts use schema types, ignore unsupported fields, and never coerce empty numbers to zero', () => {
    const schema = {fields: [{key: 'enabled', type: 'boolean'}, {key: 'limit', type: 'integer', minimum: 1, maximum: 100}, {key: 'title', type: 'string', max_length: 20}, {key: 'monthly_limit', type: 'integer', read_only: true, default_value: 3}]}
    assert.deepEqual(behavior.configValues(schema, {enabled: true, limit: 10, title: 'Home', secret: 'do not send'}), {enabled: true, limit: 10, title: 'Home'})
    assert.throws(() => behavior.configValues(schema, {enabled: true, limit: '', title: 'Home'}), /整数/)
    assert.throws(() => behavior.configValues(schema, {enabled: true, limit: 1.1, title: 'Home'}), /整数/)
    assert.deepEqual(behavior.configChanges({enabled: true, limit: 3}, {enabled: false, limit: 3}), [{key: 'enabled', before: true, after: false}])
})

test('one-time secrets are erased in place when their panel is closed', () => {
    const secret = {initial_password: 'temporary-password', code: 'invite-code', invite_url: '/register#invite=secret'}
    assert.equal(behavior.clearOneTimeSecret(secret), null)
    assert.deepEqual(secret, {})
})

test('site-wide capability switches block navigation even for an authorized administrator', () => {
    const page = {path: '/applications', meta: {requireAuth: true, capability: 'applications'}}
    assert.equal(behavior.routeDecision(page, {status: 'active', role: 'admin', capabilities: {applications: false}})?.query.feature, 'applications')
    const books = {path: '/book/list', meta: {requireAuth: true, library: true}}
    assert.equal(behavior.routeDecision(books, {status: 'active', library_enabled: true, capabilities: {library: false}})?.query.reason, 'disabled')
    const manuals = {path: '/manuals/new', fullPath: '/manuals/new', meta: {requireAuth: true, capability: 'manuals'}}
    assert.equal(behavior.routeDecision(manuals, null)?.path, '/login')
    assert.equal(behavior.routeDecision(manuals, {status: 'active', capabilities: {manuals: false}})?.query.feature, 'manuals')
    assert.equal(behavior.routeDecision(manuals, {status: 'active', capabilities: {manuals: true}}), null)
})

test('disabled modules hide their authenticated management routes', () => {
    const files = {path: '/files', fullPath: '/files', meta: {requireAuth: true, module: 'file_sharing'}}
    const user = {status: 'active'}
    assert.deepEqual(behavior.routeDecision(files, user, {file_sharing: false}), {path: '/unavailable', query: {feature: 'file_sharing'}})
    assert.equal(behavior.routeDecision(files, user, {file_sharing: true}), null)
})

test('password validation rejects NUL and follows a stronger dynamic minimum', () => {
    const value = 'long enough password' + '\u0000'
    assert.match(behavior.passwordError(value, value), /无效字符/)
    assert.match(behavior.passwordError('fifteen character', 'fifteen character', 20), /20/)
})
