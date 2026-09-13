const assert = require('node:assert/strict')
const test = require('node:test')

const {
    createWebShareApi,
} = require('../src/api/web_projects.cjs')

function createTransport(responseData = {code: 0, message: 'success', data: {ok: true}}) {
    const calls = []
    return {
        calls,
        async request(config) {
            calls.push(config)
            return {status: 200, data: responseData}
        },
    }
}

test('management requests stay same-origin and use cookie identity with CSRF protection', async () => {
    const transport = createTransport()
    const api = createWebShareApi(transport, () => 'token-value')

    const result = await api.listProjects({cursor: 'next', limit: 20, status: 'deleted'})

    assert.deepEqual(result, {ok: true})
    assert.deepEqual(transport.calls[0], {
        method: 'get',
        url: '/api/web-share',
        params: {cursor: 'next', limit: 20, status: 'deleted'},
        headers: {'X-CSRF-Token': 'token-value'},
    })
})

test('mutating an existing project sends its revision in If-Match', async () => {
    const transport = createTransport()
    const api = createWebShareApi(transport, () => 'token-value')

    await api.updateProject('12', 7, {slug: 'new-path'})
    await api.publishRelease('12', 8, '34')

    assert.equal(transport.calls[0].headers['If-Match'], '7')
    assert.equal(transport.calls[0].url, '/api/web-share/12')
    assert.deepEqual(transport.calls[1].data, {release_id: '34'})
    assert.equal(transport.calls[1].headers['If-Match'], '8')
})

test('existing content navigation confirms the cookie with me without bridging a stored token', async () => {
    const transport = createTransport()
    const api = createWebShareApi(transport, () => 'csrf')
    await api.checkBrowserSession()
    assert.deepEqual(transport.calls[0], {method: 'get', url: '/api/auth/me', headers: {'X-CSRF-Token': 'csrf'}})
})

test('checks the content Cookie with a credentialed HEAD and no passport header', async () => {
    const transport = createTransport()
    const api = createWebShareApi(transport, () => 'secret-token')

    await api.probeProjectSession('/p/report/')

    assert.deepEqual(transport.calls[0], {
        method: 'head',
        url: '/p/report/',
        withCredentials: true,
    })
})

test('surfaces the server status and message for failed envelopes', async () => {
    const transport = createTransport({code: 41002, message: 'revision conflict'})
    const api = createWebShareApi(transport, () => 'token-value')

    await assert.rejects(
        () => api.getProject('12'),
        (error) => error.code === 41002 && error.message === 'revision conflict',
    )
})
