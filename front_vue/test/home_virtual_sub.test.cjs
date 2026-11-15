const test = require('node:test')
const assert = require('node:assert/strict')
const {createHomeVirtualSubApi} = require('../src/api/home_virtual_sub.cjs')
const {subscriptionInput, createStatusLoader, workerPresentation} = require('../src/utils/home_virtual_sub_behavior.cjs')

test('management requests use fixed paths, query names and current revision', async () => {
    const requests = []
    const api = createHomeVirtualSubApi({request: async request => {requests.push(request); return {data: {code: 0, data: {saved: true}}}}}, () => 'synthetic-csrf')
    await api.detail('测试/订阅')
    await api.update('测试/订阅', 'a'.repeat(64), {urls: ['https://example.com/sub'], current_password: 'synthetic', reason: '测试'})
    await api.action('stop', '测试/订阅', 'a'.repeat(64), {current_password: 'synthetic', reason: '测试'})
    assert.equal(requests[0].url, '/api/admin/home-virtual-sub/subscriptions/detail')
    assert.equal(requests[0].params.name, '测试/订阅')
    assert.equal(requests[1].method, 'patch')
    assert.equal(requests[1].headers['If-Match'], 'a'.repeat(64))
    assert.equal(requests[1].headers['X-CSRF-Token'], 'synthetic-csrf')
    assert.equal(requests[2].url, '/api/admin/home-virtual-sub/subscriptions/stop')
    await assert.rejects(() => api.action('../../other', 'x', 'a'.repeat(64), {}))
    assert.equal(requests.length, 3)
})

test('network failure sends each mutation only once', async () => {
    let count = 0
    const api = createHomeVirtualSubApi({request: async () => {count++; throw new Error('offline')}}, () => 'csrf')
    await assert.rejects(() => api.remove('demo', 'a'.repeat(64), {}))
    assert.equal(count, 1)
})

test('editor validates fields without sending advanced credentials', () => {
    const input = subscriptionInput({name: '测试/订阅', urls_text: 'https://example.com/a\n https://example.com/b ', bind_ip: '127.0.0.1', http_port: 18301, socket_port: 18302, outbound_server_count: 3, http_proxy_chain: {password: 'secret'}})
    assert.deepEqual(input.urls, ['https://example.com/a', 'https://example.com/b'])
    assert.equal(input.http_proxy_chain, undefined)
    assert.throws(() => subscriptionInput({...input, urls_text: 'file:///etc/passwd'}))
    assert.throws(() => subscriptionInput({...input, urls_text: 'https://example.com', http_port: 18302}))
    assert.throws(() => subscriptionInput({...input, urls_text: ''}))
})

test('status loss clears online state and late requests cannot restore it', async () => {
    let status = null, failure = ''
    let resolve
    const loader = createStatusLoader(() => new Promise(done => {resolve = done}), value => {status = value}, error => {failure = error.message})
    loader.activate()
    const pending = loader.refresh()
    loader.deactivate()
    resolve({enabled: true, running: 1})
    await pending
    assert.equal(status, null)
    const offline = createStatusLoader(async () => {throw new Error('offline')}, value => {status = value}, error => {failure = error.message})
    offline.activate()
    status = {running: 1}
    await offline.refresh()
    assert.equal(status, null)
    assert.equal(failure, 'offline')
    assert.equal(workerPresentation(null).label, '状态未知')
    assert.equal(workerPresentation({state: 'running', running: true}).label, '运行中')
    assert.equal(workerPresentation({state: 'error', running: false}).label, '运行失败')
})
