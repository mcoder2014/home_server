const assert = require('node:assert/strict')
const test = require('node:test')

const {
    isSafeProjectTarget,
    normalizeInternalRedirect,
} = require('../src/utils/web_projects_navigation.cjs')

test('accepts only project content paths as open targets', () => {
    const maxLengthSlug = `a${'b'.repeat(254)}c`
    assert.equal(isSafeProjectTarget('/p/report/'), true)
    assert.equal(isSafeProjectTarget(`/p/${maxLengthSlug}/`), true)
    assert.equal(isSafeProjectTarget(`/p/${maxLengthSlug}x/`), false)
    assert.equal(isSafeProjectTarget('/p/report/assets/app.js?version=1#top'), true)
    assert.equal(isSafeProjectTarget('/p/report'), true)

    assert.equal(isSafeProjectTarget('https://evil.example/p/report/'), false)
    assert.equal(isSafeProjectTarget('//evil.example/p/report/'), false)
    assert.equal(isSafeProjectTarget('/p/report\\evil'), false)
    assert.equal(isSafeProjectTarget('/p/report/\nnext'), false)
    assert.equal(isSafeProjectTarget('/p/report/%5cevil'), false)
    assert.equal(isSafeProjectTarget('/p/report/%0anext'), false)
    assert.equal(isSafeProjectTarget('/book/list'), false)
    assert.equal(isSafeProjectTarget('/p/a/'), false)
    assert.equal(isSafeProjectTarget('/p/ab/'), false)
})

// 验证登录回跳仅保留允许的业务页和托管路径，拒绝外部或混淆地址，同时保留合法查询参数。
test('limits login redirects to known application pages and project content paths', () => {
    assert.equal(normalizeInternalRedirect('/applications'), '/applications')
    assert.equal(normalizeInternalRedirect('/web-share'), '/web-share')
    assert.equal(normalizeInternalRedirect('/web-share/123?tab=releases'), '/web-share/123?tab=releases')
    assert.equal(
        normalizeInternalRedirect('/web-share/open?target=%2Fp%2Freport%2F'),
        '/web-share/open?target=%2Fp%2Freport%2F',
    )
    assert.equal(normalizeInternalRedirect('/web-projects'), '/web-projects')
    assert.equal(normalizeInternalRedirect('/web-projects/123?tab=releases'), '/web-projects/123?tab=releases')
    assert.equal(
        normalizeInternalRedirect('/web-projects/open?target=%2Fp%2Freport%2F'),
        '/web-projects/open?target=%2Fp%2Freport%2F',
    )
    assert.equal(normalizeInternalRedirect('/p/report/'), '/p/report/')

    assert.equal(normalizeInternalRedirect('/web-projects-evil'), '/')
    assert.equal(normalizeInternalRedirect('/web-share-evil'), '/')
    assert.equal(normalizeInternalRedirect('//evil.example'), '/')
    assert.equal(normalizeInternalRedirect('https://evil.example'), '/')
    assert.equal(normalizeInternalRedirect('/web-projects\\evil'), '/')
    assert.equal(normalizeInternalRedirect('/web-projects/%5cevil'), '/')
    assert.equal(normalizeInternalRedirect('/web-projects/%0anext'), '/')
    assert.equal(normalizeInternalRedirect('/book/list'), '/book/list')
    assert.equal(normalizeInternalRedirect('/account/security'), '/account/security')
    assert.equal(normalizeInternalRedirect('/admin/config'), '/admin/config')
    assert.equal(normalizeInternalRedirect('/invitations'), '/invitations')
    assert.equal(normalizeInternalRedirect('/api/admin/users'), '/')
    assert.equal(normalizeInternalRedirect('/admin-evil'), '/')
})

test('preserves a project bookmark fragment through the login redirect', () => {
    assert.equal(
        normalizeInternalRedirect('/p/report/?view=1', '#/history'),
        '/p/report/?view=1#/history',
    )
    assert.equal(normalizeInternalRedirect('/p/report/#own', '#/history'), '/p/report/#own')
    assert.equal(normalizeInternalRedirect('/p/report/', '#%0anext'), '/')
})
