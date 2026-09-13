const assert = require('node:assert/strict')
const test = require('node:test')

const {createApplicationsApi} = require('../src/api/applications.cjs')

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

test('application management requests use the browser CSRF header without owner or secret fields', async () => {
    const transport = createTransport()
    const api = createApplicationsApi(transport, () => 'user-token')

    await api.listApplications({cursor: 'next', limit: 20})
    await api.createApplication({
        name: 'deploy bot',
        description: 'publishes tools',
        scopes: ['web-projects:read'],
        expires_in_days: 90,
    })

    assert.deepEqual(transport.calls[0], {
        method: 'get',
        url: '/api/applications',
        params: {cursor: 'next', limit: 20},
        headers: {'X-CSRF-Token': 'user-token'},
    })
    assert.deepEqual(transport.calls[1], {
        method: 'post',
        url: '/api/applications',
        data: {
            name: 'deploy bot',
            description: 'publishes tools',
            scopes: ['web-projects:read'],
            expires_in_days: 90,
        },
        headers: {'X-CSRF-Token': 'user-token'},
    })
    assert.equal('owner_id' in transport.calls[1].data, false)
    assert.equal('secret_key' in transport.calls[1].data, false)
})

test('application mutations send the current revision in If-Match', async () => {
    const transport = createTransport()
    const api = createApplicationsApi(transport, () => 'user-token')

    await api.updateApplication('app/12', 7, {name: 'renamed'})
    await api.rotateSecret('app/12', 8)
    await api.revokeApplication('app/12', 9)

    assert.equal(transport.calls[0].url, '/api/applications/app%2F12')
    assert.equal(transport.calls[0].headers['If-Match'], '7')
    assert.equal(transport.calls[1].url, '/api/applications/app%2F12/rotate')
    assert.equal(transport.calls[1].headers['If-Match'], '8')
    assert.equal(transport.calls[2].method, 'delete')
    assert.equal(transport.calls[2].headers['If-Match'], '9')
})

test('application API preserves identity failures for the page to handle', async () => {
    const transport = {
        async request() {
            const error = new Error('request rejected')
            error.response = {status: 401, data: {code: 40101, message: 'please sign in'}}
            throw error
        },
    }
    const api = createApplicationsApi(transport, () => 'expired-token')

    await assert.rejects(
        () => api.listApplications({limit: 20}),
        (error) => error.status === 401 && error.code === 40101 && error.message === 'please sign in',
    )
})

test('failed response envelopes do not expose response data as a successful list', async () => {
    const transport = createTransport({code: 40401, message: 'application not found', data: {items: [{secret_key: 'must-not-render'}]}})
    const api = createApplicationsApi(transport, () => 'user-token')

    await assert.rejects(
        () => api.listApplications({limit: 20}),
        (error) => error.status === 200 && error.code === 40401 && error.message === 'application not found',
    )
})
