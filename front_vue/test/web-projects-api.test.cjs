const assert = require('node:assert/strict')
const test = require('node:test')

const {
    createWebShareApi,
    synchronizeBrowserIdentity,
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

test('management requests stay same-origin and carry the passport token', async () => {
    const transport = createTransport()
    const api = createWebShareApi(transport, () => 'token-value')

    const result = await api.listProjects({cursor: 'next', limit: 20, status: 'deleted'})

    assert.deepEqual(result, {ok: true})
    assert.deepEqual(transport.calls[0], {
        method: 'get',
        url: '/api/web-share',
        params: {cursor: 'next', limit: 20, status: 'deleted'},
        headers: {passport: 'token-value'},
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

test('browser login is a same-origin POST with no token in its URL or body', async () => {
    const transport = createTransport()
    const api = createWebShareApi(transport, () => 'secret-token')

    await api.createBrowserLogin()

    assert.deepEqual(transport.calls[0], {
        method: 'post',
        url: '/api/auth/browser-login',
        headers: {passport: 'secret-token'},
    })
})

test('browser login can explicitly use the newly returned token', async () => {
    const transport = createTransport()
    const api = createWebShareApi(transport, () => 'old-token')

    await api.createBrowserLogin('new-token')

    assert.deepEqual(transport.calls[0], {
        method: 'post',
        url: '/api/auth/browser-login',
        headers: {passport: 'new-token'},
    })
})

test('ordinary login synchronizes the browser identity before committing it locally', async () => {
    const events = []
    const api = {
        async createBrowserLogin(token) {
            events.push(`sync:${token}`)
        },
    }

    const available = await synchronizeBrowserIdentity(api, {token: 'new-token', user_name: 'user-b'}, (identity) => {
        events.push(`commit:${identity.user_name}`)
    })

    assert.equal(available, true)
    assert.deepEqual(events, ['sync:new-token', 'commit:user-b'])
})

test('browser-login 404 keeps legacy book login available', async () => {
    let committedUser = ''
    const api = {
        async createBrowserLogin() {
            const error = new Error('not found')
            error.status = 404
            throw error
        },
    }

    const available = await synchronizeBrowserIdentity(api, {token: 'new-token', user_name: 'legacy-user'}, (identity) => {
        committedUser = identity.user_name
    })

    assert.equal(available, false)
    assert.equal(committedUser, 'legacy-user')
})

test('browser-login failure does not commit the new identity', async () => {
    let committed = false
    const api = {
        async createBrowserLogin() {
            const error = new Error('service unavailable')
            error.status = 503
            throw error
        },
    }

    await assert.rejects(
        () => synchronizeBrowserIdentity(api, {token: 'new-token', user_name: 'user-b'}, () => {
            committed = true
        }),
        /service unavailable/,
    )
    assert.equal(committed, false)
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
