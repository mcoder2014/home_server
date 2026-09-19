const assert = require('node:assert/strict')
const test = require('node:test')

const {
    expirationValue,
    fileShareState,
    parseDownloadName,
    randomShareCode,
    resourcePasswordError,
    secretError,
    uploadFileError,
} = require('../src/utils/file_sharing_behavior.cjs')

test('random share codes use exactly six case-sensitive ASCII letters and digits without modulo bias', () => {
    const batches = [
        new Uint8Array([0, 61, 51, 248, 255, 1, 248, 248, 248, 248, 248, 248]),
        new Uint8Array([2, 3, 4, 248, 248, 248, 248, 248, 248, 248, 248, 248]),
    ]
    const code = randomShareCode(array => array.set(batches.shift()))
    assert.equal(code, 'A9zBCD')
    assert.match(code, /^[A-Za-z0-9]{6}$/)
})

test('share secrets enforce the mode-specific byte limits', () => {
    assert.equal(secretError('none', ''), '')
    assert.equal(secretError('code', 'aB09zZ'), '')
    assert.match(secretError('code', 'abcdefg'), /6 位/)
    assert.equal(secretError('password', '八字密码'), '')
    assert.match(secretError('password', 'short'), /8.*72/)
    assert.equal(secretError('password', '😀'.repeat(18)), '')
    assert.match(secretError('password', '😀'.repeat(19)), /72/)
})

test('resource passwords validate confirmation and UTF-8 byte length independently from account passwords', () => {
    assert.equal(resourcePasswordError('说明书密码', '说明书密码'), '')
    assert.match(resourcePasswordError('short', 'short'), /8.*72/)
    assert.match(resourcePasswordError('😀'.repeat(19), '😀'.repeat(19)), /72/)
    assert.match(resourcePasswordError('家庭访问密码', '另一个密码'), /不一致/)
})

test('upload and expiration validation match the 50 MiB and preset contract', () => {
    assert.match(uploadFileError(null), /选择/)
    assert.equal(uploadFileError({name: 'empty.txt', size: 0}), '')
    assert.match(uploadFileError({name: 'broken.bin', size: -1}), /大小无效/)
    assert.match(uploadFileError({name: 'broken.bin', size: Number.NaN}), /大小无效/)
    assert.equal(uploadFileError({name: 'ok.bin', size: 50 * 1024 * 1024}), '')
    assert.match(uploadFileError({name: 'large.bin', size: 50 * 1024 * 1024 + 1}), /50 MiB/)

    const now = new Date('2026-09-19T00:00:00.000Z')
    assert.equal(expirationValue('never', '', now), null)
    assert.equal(expirationValue('1d', '', now), '2026-09-20T00:00:00.000Z')
    assert.equal(expirationValue('7d', '', now), '2026-09-26T00:00:00.000Z')
    assert.equal(expirationValue('30d', '', now), '2026-10-19T00:00:00.000Z')
    assert.equal(expirationValue('custom', '2026-09-21T08:30', now), new Date('2026-09-21T08:30').toISOString())
    assert.throws(() => expirationValue('custom', '2026-09-18T23:59', now), /未来/)
})

test('share state prioritizes revoked, expired and exhausted records and keeps counts visible', () => {
    const now = new Date('2026-09-19T00:00:00Z')
    assert.deepEqual(fileShareState({revoked_at: '2026-09-18T00:00:00Z', download_count: 2, max_downloads: 3}, now), {key: 'revoked', label: '已撤销', type: 'info'})
    assert.equal(fileShareState({expires_at: '2026-09-18T00:00:00Z'}, now).key, 'expired')
    assert.equal(fileShareState({download_count: 3, max_downloads: 3}, now).key, 'exhausted')
    assert.equal(fileShareState({download_count: 2, max_downloads: 3}, now).key, 'active')
})

test('download filename prefers UTF-8 Content-Disposition and rejects path components', () => {
    assert.equal(parseDownloadName("attachment; filename*=UTF-8''%E5%AE%B6%E5%BA%AD%20%E8%B4%A6%E5%8D%95.pdf", 'fallback.bin'), '家庭 账单.pdf')
    assert.equal(parseDownloadName('attachment; filename="report.txt"', 'fallback.bin'), 'report.txt')
    assert.equal(parseDownloadName('attachment; filename="../secret.txt"', 'fallback.bin'), 'secret.txt')
    assert.equal(parseDownloadName('', 'fallback.bin'), 'fallback.bin')
})
