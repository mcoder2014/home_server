const MAX_FILE_SIZE = 50 * 1024 * 1024
const CODE_CHARACTERS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789'

function utf8Length(value) {
    return new TextEncoder().encode(String(value || '')).length
}

function uploadFileError(file) {
    if (!file) return '请选择要上传的文件'
    const size = Number(file.size)
    if (!Number.isFinite(size) || size < 0) return '文件大小无效'
    if (size > MAX_FILE_SIZE) return '单个文件不能超过 50 MiB'
    return ''
}

function secretError(mode, value, minimum = 8, codeLength = 6) {
    const secret = String(value || '')
    if (mode === 'none') return ''
    if (mode === 'code') return /^[A-Za-z0-9]+$/.test(secret) && secret.length === codeLength ? '' : `分享码必须是 ${codeLength} 位 ASCII 字母或数字，区分大小写`
    if (mode !== 'password') return '请选择有效的口令方式'
    const bytes = utf8Length(secret)
    return bytes >= minimum && bytes <= 72 ? '' : `登录分享密码必须为 ${minimum}～72 个 UTF-8 字节`
}

function resourcePasswordError(password, confirmation, minimum = 8) {
    const bytes = utf8Length(password)
    if (bytes < minimum || bytes > 72) return `阅读密码必须为 ${minimum}～72 个 UTF-8 字节`
    if (password !== confirmation) return '两次输入的阅读密码不一致'
    return ''
}

function randomShareCode(fillRandom = array => crypto.getRandomValues(array), length = 6) {
    let code = ''
    while (code.length < length) {
        const bytes = new Uint8Array(12)
        fillRandom(bytes)
        for (const byte of bytes) {
            if (byte >= 248) continue
            code += CODE_CHARACTERS[byte % CODE_CHARACTERS.length]
            if (code.length === length) break
        }
    }
    return code
}

function expirationValue(preset, customValue, now = new Date()) {
    if (preset === 'never') return null
    const days = { '1d': 1, '7d': 7, '30d': 30 }[preset]
    let expiration
    if (days) expiration = new Date(now.getTime() + days * 86400000)
    else if (preset === 'custom') expiration = new Date(customValue)
    else throw new Error('请选择有效期')
    if (Number.isNaN(expiration.getTime()) || expiration <= now) throw new Error('自定义到期时间必须在未来')
    return expiration.toISOString()
}

function fileShareState(share, now = new Date()) {
    if (share && share.revoked_at) return {key: 'revoked', label: '已撤销', type: 'info'}
    if (share && share.expires_at && new Date(share.expires_at) <= now) return {key: 'expired', label: '已过期', type: 'warning'}
    if (share && Number(share.max_downloads) > 0 && Number(share.download_count) >= Number(share.max_downloads)) return {key: 'exhausted', label: '次数已用完', type: 'warning'}
    return {key: 'active', label: '可用', type: 'success'}
}

function parseDownloadName(disposition, fallback = 'download') {
    const value = String(disposition || '')
    let name = ''
    const encoded = value.match(/filename\*\s*=\s*UTF-8''([^;]+)/i)
    if (encoded) {
        try { name = decodeURIComponent(encoded[1].trim()) } catch (_) { name = '' }
    }
    if (!name) {
        const plain = value.match(/filename\s*=\s*(?:"([^"]*)"|([^;]+))/i)
        name = plain ? (plain[1] || plain[2] || '').trim() : ''
    }
    // eslint-disable-next-line no-control-regex
    name = name.split(/[\\/]/).pop().replace(/[\u0000-\u001f\u007f]/g, '').trim()
    return name || String(fallback || 'download')
}

module.exports = {
    MAX_FILE_SIZE,
    expirationValue,
    fileShareState,
    parseDownloadName,
    randomShareCode,
    resourcePasswordError,
    secretError,
    uploadFileError,
    utf8Length,
}
