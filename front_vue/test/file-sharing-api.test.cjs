const assert = require('node:assert/strict')
const test = require('node:test')

const {createFilesApi} = require('../src/api/files.cjs')

function transport(responseData = {code: 0, data: {ok: true}}) {
    const calls = []
    return {calls, async request(config) { calls.push(config); return {status: 200, data: responseData, headers: {}} }}
}

test('file owner endpoints preserve string identifiers, cursor fields and CSRF headers', async () => {
    const client = transport()
    const api = createFilesApi(client, () => 'csrf')
    await api.listFiles({cursor: '9223372036854775807', limit: 20})
    await api.listEligibleUsers()
    await api.getFile('9223372036854775807')
    await api.deleteFile('9223372036854775807')
    await api.listShares('9223372036854775807', {cursor: '9', limit: 20})
    await api.revokeShare('9223372036854775807', '18446744073709551615')

    assert.deepEqual(client.calls[0], {method: 'get', url: '/api/files', params: {cursor: '9223372036854775807', limit: 20}, headers: {'X-CSRF-Token': 'csrf'}})
    assert.deepEqual(client.calls[1], {method: 'get', url: '/api/files/eligible-users', headers: {'X-CSRF-Token': 'csrf'}})
    assert.equal(client.calls[2].url, '/api/files/9223372036854775807')
    assert.equal(client.calls[3].method, 'delete')
    assert.deepEqual(client.calls[4].params, {cursor: '9', limit: 20})
    assert.equal(client.calls[5].url, '/api/files/9223372036854775807/shares/18446744073709551615')
})

test('upload is multipart and reports the file-specific 50 MiB limit', async () => {
    const client = transport()
    const api = createFilesApi(client, () => 'csrf')
    const file = new Blob(['content'], {type: 'text/plain'})
    const progress = () => {}
    await api.uploadFile(file, progress)
    assert.equal(client.calls[0].method, 'post')
    assert.equal(client.calls[0].url, '/api/files')
    assert.equal(client.calls[0].data.get('file').size, file.size)
    assert.equal(client.calls[0].onUploadProgress, progress)

    const rejected = createFilesApi({async request() {
        const error = new Error('too large'); error.response = {status: 413, data: {code: 6}}; throw error
    }}, () => 'csrf')
    await assert.rejects(() => rejected.uploadFile(file), error => error.status === 413 && /50 MiB/.test(error.message) && !/头像/.test(error.message))
})

test('share creation sends combined access, secret, expiration and count policy', async () => {
    const client = transport({code: 0, data: {share: {id: '3'}, secret: 'aB09zZ'}})
    const api = createFilesApi(client, () => 'csrf')
    const payload = {access_mode: 'members', member_user_ids: ['42'], secret_mode: 'code', secret: '', expires_at: '2026-09-26T00:00:00Z', max_downloads: 3}
    const result = await api.createShare('9', payload)
    assert.equal(result.secret, 'aB09zZ')
    assert.deepEqual(client.calls[0], {method: 'post', url: '/api/files/9/shares', data: payload, headers: {'X-CSRF-Token': 'csrf'}})
})

test('recipient status and unlock remain same-origin while download makes one POST blob request', async () => {
    const client = transport({code: 0, data: {state: 'locked'}})
    const api = createFilesApi(client, () => '')
    await api.getShare('Token_-09')
    await api.unlockShare('Token_-09', 'secret value')
    await api.downloadShare('Token_-09')
    assert.deepEqual(client.calls[0], {method: 'get', url: '/api/file-shares/Token_-09', headers: {}})
    assert.deepEqual(client.calls[1], {method: 'post', url: '/api/file-shares/Token_-09/unlock', data: {secret: 'secret value'}, headers: {}})
    assert.deepEqual(client.calls[2], {method: 'post', url: '/api/file-shares/Token_-09/download', data: null, responseType: 'blob', headers: {}})
    assert.equal(client.calls.filter(call => call.url.endsWith('/download')).length, 1)
})

test('locked 403 remains a resource error instead of becoming an identity failure', async () => {
    let identityFailures = 0
    const browserClient = require('../src/api/browser_client.cjs')
    browserClient.configureBrowserSession(() => 'csrf', () => { identityFailures++ })
    const api = createFilesApi({async request() {
        const error = new Error('locked'); error.response = {status: 403, data: {code: 3, message: '需要口令'}}; throw error
    }}, () => 'csrf')
    await assert.rejects(() => api.getShare('locked-token'), error => error.status === 403 && error.code === 3)
    assert.equal(identityFailures, 0)
})

test('download decodes a JSON error envelope returned through the blob response type', async () => {
    const api = createFilesApi({async request() {
        const error = new Error('blob response')
        error.response = {status: 404, data: new Blob([JSON.stringify({code: 4, message: '分享链接不可用'})], {type: 'application/json'})}
        throw error
    }}, () => '')

    await assert.rejects(
        () => api.downloadShare('expired-token'),
        error => error.status === 404 && error.code === 4 && error.message === '分享链接不可用',
    )
})
