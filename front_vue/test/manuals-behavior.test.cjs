const assert = require('node:assert/strict')
const test = require('node:test')

const {
    appendFileItems,
    appendStructuredItem,
    moveItem,
    normalizeManualCategories,
    retryFailedItem,
    safeExternalURL,
    uploadQueuedItems,
    validateManualFile,
} = require('../src/utils/manuals_behavior.cjs')

function file(name, type, size = 10, lastModified = 1) {
    return {name, type, size, lastModified}
}

test('multiple file selections append in order, keep duplicate names, and retain stable request ids', () => {
    let sequence = 0
    const requestID = () => `request-${++sequence}`
    let result = appendFileItems([], [file('front.jpg', 'image/jpeg'), file('guide.pdf', 'application/pdf')], 0, requestID)
    result = appendFileItems(result.items, [file('front.jpg', 'image/jpeg')], 0, requestID)

    assert.deepEqual(result.items.map(item => item.file.name), ['front.jpg', 'guide.pdf', 'front.jpg'])
    assert.deepEqual(result.items.map(item => item.client_request_id), ['request-1', 'request-2', 'request-3'])
    assert.equal(result.items[2].duplicate, true)
    assert.equal(result.errors.length, 0)
})

test('manual files enforce 50 MiB, supported formats, HEIC guidance, and the 100-item total', () => {
    assert.equal(validateManualFile(file('photo.webp', 'image/webp', 50 * 1024 * 1024)), '')
    assert.match(validateManualFile(file('photo.heic', 'image/heic')), /转换.*JPEG.*PNG/)
    assert.match(validateManualFile(file('archive.zip', 'application/zip')), /只支持/)
    assert.match(validateManualFile(file('large.pdf', 'application/pdf', 50 * 1024 * 1024 + 1)), /50 MiB/)

    const queue = Array.from({length: 99}, (_, index) => ({status: 'waiting', client_request_id: `old-${index}`}))
    const result = appendFileItems(queue, [file('one.jpg', 'image/jpeg'), file('two.jpg', 'image/jpeg')], 0, () => 'new')
    assert.equal(result.items.length, 100)
    assert.match(result.errors[0], /100/)
})

test('partial upload keeps successes, retries only failures, and reuses the idempotency key', async () => {
    let sequence = 0
    let queue = appendFileItems([], [
        file('one.jpg', 'image/jpeg'),
        file('two.jpg', 'image/jpeg'),
        file('three.jpg', 'image/jpeg'),
    ], 0, () => `request-${++sequence}`).items
    const attempts = []

    await uploadQueuedItems(queue, async item => {
        attempts.push(item.client_request_id)
        if (item.client_request_id === 'request-2') throw new Error('合成失败')
        return {item: {id: `server-${item.client_request_id}`}, revision: attempts.length + 1}
    })

    assert.deepEqual(queue.map(item => item.status), ['success', 'failed', 'success'])
    assert.equal(queue[1].attempted, true)
    assert.deepEqual(queue.map(item => item.client_request_id), ['request-1', 'request-2', 'request-3'])
    retryFailedItem(queue[1])
    assert.equal(queue[1].attempted, true)
    await uploadQueuedItems(queue, async item => {
        attempts.push(item.client_request_id)
        return {item: {id: 'server-request-2'}, revision: 5}
    })
    assert.deepEqual(attempts, ['request-1', 'request-2', 'request-3', 'request-2'])
    assert.deepEqual(queue.map(item => item.status), ['success', 'success', 'success'])
})

test('text and URL entries remain editable queue items and move without changing request ids', () => {
    const first = appendStructuredItem([], 'text', 0, () => 'text-1').items
    first[0].title = '保养'
    first[0].text = '第一步\n第二步'
    const second = appendStructuredItem(first, 'url', 0, () => 'url-1').items
    second[1].url = 'https://example.com/help'
    const moved = moveItem(second, 1, -1)

    assert.deepEqual(moved.map(item => item.client_request_id), ['url-1', 'text-1'])
    assert.equal(moved[1].text, '第一步\n第二步')
    assert.equal(safeExternalURL('https://example.com/help'), 'https://example.com/help')
    assert.equal(safeExternalURL('javascript:alert(1)'), '')
    assert.equal(safeExternalURL('https://user:secret@example.com/'), '')
    assert.equal(safeExternalURL('https://example.com/\nnext'), '')
})

test('manual categories trim and deduplicate, while enforcing 20 values and 100 characters each', () => {
    assert.deepEqual(normalizeManualCategories([' 厨房 ', '家电', '厨房', '']), ['厨房', '家电'])
    assert.deepEqual(normalizeManualCategories([]), [])
    assert.throws(() => normalizeManualCategories(Array.from({length: 21}, (_, index) => `分类${index}`)), /20/)
    assert.throws(() => normalizeManualCategories(['类'.repeat(101)]), /100/)
})
