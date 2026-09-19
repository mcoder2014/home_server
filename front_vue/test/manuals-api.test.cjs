const assert = require('node:assert/strict')
const test = require('node:test')

const {createManualsApi} = require('../src/api/manuals.cjs')

function createTransport(responseData = {code: 0, data: {ok: true}}) {
    const calls = []
    return {
        calls,
        async request(config) {
            calls.push(config)
            return {status: 200, data: responseData}
        },
    }
}

test('manual list and categories keep anonymous filters and 20-item cursor parameters', async () => {
    const transport = createTransport()
    const api = createManualsApi(transport, () => '')

    await api.listManuals({q: '咖啡', category: '厨房', mine: false, cursor: '88', limit: 20})
    await api.listCategories({mine: false})

    assert.deepEqual(transport.calls[0], {
        method: 'get',
        url: '/api/manuals',
        params: {q: '咖啡', category: '厨房', mine: false, cursor: '88', limit: 20},
        headers: {},
    })
    assert.deepEqual(transport.calls[1], {
        method: 'get',
        url: '/api/manuals/categories',
        params: {mine: false},
        headers: {},
    })
})

test('category suggestions merge every cursor page and reject a cursor that makes no progress', async () => {
    const calls = []
    const pages = [
        {code: 0, data: {items: ['A', 'B'], has_more: true, next_cursor: 'B'}},
        {code: 0, data: {items: ['C'], has_more: false, next_cursor: ''}},
    ]
    const transport = {async request(config) { calls.push(config); return {status: 200, data: pages.shift()} }}
    const api = createManualsApi(transport, () => '')

    const result = await api.listAllCategories({mine: true})

    assert.deepEqual(result.items, ['A', 'B', 'C'])
    assert.deepEqual(calls.map(call => call.params), [
        {mine: true, limit: 1000},
        {mine: true, limit: 1000, cursor: 'B'},
    ])

    const stalled = createManualsApi({async request() {
        return {status: 200, data: {code: 0, data: {items: ['A'], has_more: true, next_cursor: ''}}}
    }}, () => '')
    await assert.rejects(() => stalled.listAllCategories({mine: false}), /游标未前进/)
})

test('manual mutations put revision in JSON and preserve string identifiers', async () => {
    const transport = createTransport()
    const api = createManualsApi(transport, () => 'csrf-value')

    await api.createManual({name: '冰箱', categories: ['厨房', '家电'], access_mode: 'owner', client_request_id: 'create-1'})
    await api.updateManual('9223372036854775807', {revision: 7, categories: ['厨房'], status: 'active', item_ids: ['9']})
    await api.deleteManual('9223372036854775807', 8)
    await api.addItem('9223372036854775807', {kind: 'text', text: '保养', client_request_id: 'text-1'})
    await api.deleteItem('9223372036854775807', '18446744073709551615', 9)

    assert.equal(transport.calls[0].headers['X-CSRF-Token'], 'csrf-value')
    assert.deepEqual(transport.calls[0].data.categories, ['厨房', '家电'])
    assert.equal('category' in transport.calls[0].data, false)
    assert.deepEqual(transport.calls[1], {
        method: 'patch',
        url: '/api/manuals/9223372036854775807',
        data: {revision: 7, categories: ['厨房'], status: 'active', item_ids: ['9']},
        headers: {'X-CSRF-Token': 'csrf-value'},
    })
    assert.deepEqual(transport.calls[2].data, {revision: 8})
    assert.equal(typeof transport.calls[2].data.revision, 'number')
    assert.equal(transport.calls[3].url, '/api/manuals/9223372036854775807/items')
    assert.deepEqual(transport.calls[4], {
        method: 'delete',
        url: '/api/manuals/9223372036854775807/items/18446744073709551615',
        data: {revision: 9},
        headers: {'X-CSRF-Token': 'csrf-value'},
    })
    assert.equal(typeof transport.calls[4].data.revision, 'number')
})

test('file upload sends title and stable request id as multipart with progress reporting', async () => {
    const transport = createTransport()
    const api = createManualsApi(transport, () => 'csrf-value')
    const file = new Blob(['pdf bytes'], {type: 'application/pdf'})
    const progress = () => {}

    await api.uploadFile('manual/1', file, 'Quick start', 'upload-1', progress)

    const call = transport.calls[0]
    assert.equal(call.method, 'post')
    assert.equal(call.url, '/api/manuals/manual%2F1/files')
    assert.equal(call.headers['X-CSRF-Token'], 'csrf-value')
    assert.equal(call.data.get('file').size, file.size)
    assert.equal(call.data.get('file').type, file.type)
    assert.equal(call.data.get('title'), 'Quick start')
    assert.equal(call.data.get('client_request_id'), 'upload-1')
    assert.equal(call.onUploadProgress, progress)
})

test('manual upload overrides the avatar-specific 413 message with the 50 MiB manual limit', async () => {
    const transport = {
        async request() {
            const error = new Error('rejected')
            error.response = {status: 413, data: {}}
            throw error
        },
    }
    const api = createManualsApi(transport, () => 'csrf-value')

    await assert.rejects(
        () => api.uploadFile('1', new Blob(['x']), '', 'upload-1'),
        error => error.status === 413 && /50 MiB/.test(error.message) && !/2 MiB/.test(error.message),
    )
})

test('manual passwords are managed separately from visibility and unlock with a resource cookie', async () => {
    const transport = createTransport({code: 0, data: {password_protected: true, version: 5}})
    const api = createManualsApi(transport, () => 'csrf-value')
    await api.getPassword('9223372036854775807')
    await api.setPassword('9223372036854775807', {password: '说明书阅读密码', version: 4})
    await api.unlockManual('9223372036854775807', '说明书阅读密码')

    assert.deepEqual(transport.calls[0], {
        method: 'get', url: '/api/manuals/9223372036854775807/password', headers: {'X-CSRF-Token': 'csrf-value'},
    })
    assert.deepEqual(transport.calls[1], {
        method: 'put', url: '/api/manuals/9223372036854775807/password',
        data: {password: '说明书阅读密码', version: 4}, headers: {'X-CSRF-Token': 'csrf-value'},
    })
    assert.deepEqual(transport.calls[2], {
        method: 'post', url: '/api/manuals/9223372036854775807/unlock',
        data: {password: '说明书阅读密码'}, headers: {'X-CSRF-Token': 'csrf-value'},
    })
})
