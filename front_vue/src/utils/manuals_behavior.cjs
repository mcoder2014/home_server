const MAX_ITEMS = 100
const MAX_FILE_SIZE = 50 * 1024 * 1024
const IMAGE_TYPES = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp'])
const IMAGE_EXTENSIONS = new Set(['jpg', 'jpeg', 'png', 'gif', 'webp'])

function createRequestID() {
    if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID()
    if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
        const bytes = new Uint8Array(16)
        crypto.getRandomValues(bytes)
        return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
    }
    return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}`
}

function extension(name) {
    const match = String(name || '').toLowerCase().match(/\.([^.]+)$/)
    return match ? match[1] : ''
}

function validateManualFile(file) {
    if (!file || !file.name || !Number.isFinite(Number(file.size)) || Number(file.size) <= 0) return '请选择非空文件'
    if (Number(file.size) > MAX_FILE_SIZE) return '单个文件不能超过 50 MiB'
    const suffix = extension(file.name)
    const type = String(file.type || '').toLowerCase()
    if (['heic', 'heif'].includes(suffix) || ['image/heic', 'image/heif'].includes(type)) {
        return 'HEIC/HEIF 图片暂不支持，请转换为 JPEG 或 PNG 后重试'
    }
    if (IMAGE_TYPES.has(type) || IMAGE_EXTENSIONS.has(suffix)) return ''
    if (type === 'application/pdf' || suffix === 'pdf' || type === 'text/plain' || suffix === 'txt') return ''
    return '只支持 JPG、PNG、GIF、WebP、PDF 和 TXT 文件'
}

function fileKind(file) {
    return IMAGE_TYPES.has(String(file.type || '').toLowerCase()) || IMAGE_EXTENSIONS.has(extension(file.name)) ? 'image' : 'file'
}

function duplicateFile(file, items) {
    return items.some(item => item.file
        && item.file.name === file.name
        && Number(item.file.size) === Number(file.size)
        && Number(item.file.lastModified || 0) === Number(file.lastModified || 0))
}

function pendingCount(items) {
    return items.filter(item => item.status !== 'success').length
}

function appendFileItems(items, files, persistedCount = 0, makeRequestID = createRequestID) {
    const next = items.slice()
    const errors = []
    for (const file of Array.from(files || [])) {
        const error = validateManualFile(file)
        if (error) {
            errors.push(`${file && file.name || '未知文件'}：${error}`)
            continue
        }
        if (persistedCount + pendingCount(next) >= MAX_ITEMS) {
            errors.push(`每份说明书最多 100 项资料，${file.name} 未加入`)
            continue
        }
        const clientRequestID = makeRequestID()
        next.push({
            local_id: clientRequestID,
            client_request_id: clientRequestID,
            kind: fileKind(file),
            file,
            title: file.name,
            duplicate: duplicateFile(file, next),
            status: 'waiting',
            attempted: false,
            progress: 0,
            error: '',
            server_item: null,
        })
    }
    return {items: next, errors}
}

function appendStructuredItem(items, kind, persistedCount = 0, makeRequestID = createRequestID) {
    const next = items.slice()
    if (!['text', 'url'].includes(kind)) return {items: next, errors: ['只能添加文本或网页链接']}
    if (persistedCount + pendingCount(next) >= MAX_ITEMS) return {items: next, errors: ['每份说明书最多 100 项资料']}
    const clientRequestID = makeRequestID()
    next.push({
        local_id: clientRequestID,
        client_request_id: clientRequestID,
        kind,
        title: '',
        text: '',
        url: '',
        status: 'waiting',
        attempted: false,
        progress: 0,
        error: '',
        server_item: null,
    })
    return {items: next, errors: []}
}

function moveItem(items, index, offset) {
    const target = index + offset
    if (index < 0 || index >= items.length || target < 0 || target >= items.length) return items.slice()
    const next = items.slice()
    const [item] = next.splice(index, 1)
    next.splice(target, 0, item)
    return next
}

function retryFailedItem(item) {
    if (item && item.status === 'failed') {
        item.status = 'waiting'
        item.progress = 0
        item.error = ''
    }
    return item
}

async function uploadQueuedItems(items, upload, onChange = () => {}) {
    for (let index = 0; index < items.length; index++) {
        const item = items[index]
        if (item.status !== 'waiting') continue
        item.status = 'uploading'
        item.attempted = true
        item.progress = 0
        item.error = ''
        onChange(item, index)
        try {
            const result = await upload(item, index)
            item.status = 'success'
            item.progress = 100
            item.server_item = result && result.item || null
            item.revision = result && result.revision
        } catch (error) {
            item.status = 'failed'
            item.error = error && error.message || '上传失败'
        }
        onChange(item, index)
    }
    return items
}

function safeExternalURL(value) {
    const text = String(value || '').trim()
    // eslint-disable-next-line no-control-regex
    if (/[\u0000-\u001f\u007f]/.test(text)) return ''
    try {
        const parsed = new URL(text)
        if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password) return ''
        return text
    } catch (error) {
        return ''
    }
}

function normalizeManualCategories(values) {
    const categories = [...new Set((values || []).map(value => String(value).trim()).filter(Boolean))]
    if (categories.length > 20) throw new Error('每份说明书最多选择 20 个分类')
    if (categories.some(value => Array.from(value).length > 100)) throw new Error('单个分类不能超过 100 个字符')
    return categories
}

module.exports = {
    MAX_FILE_SIZE,
    MAX_ITEMS,
    appendFileItems,
    appendStructuredItem,
    createRequestID,
    moveItem,
    normalizeManualCategories,
    retryFailedItem,
    safeExternalURL,
    uploadQueuedItems,
    validateManualFile,
}
