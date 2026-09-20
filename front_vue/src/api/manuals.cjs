const axios = require('axios')
const {browserHeaders, requestError} = require('./browser_client.cjs')

function envelopeError(response) {
    const body = response && response.data ? response.data : {}
    const error = new Error(body.message || body.msg || '请求失败')
    error.code = body.code
    error.status = response && response.status
    return error
}

function manualRequestError(error, sentCSRF, upload) {
    if (upload && error && error.response && error.response.status === 413) {
        const normalized = new Error('文件上传失败：说明书单个文件不能超过 50 MiB')
        normalized.code = error.response.data && error.response.data.code
        normalized.status = 413
        return normalized
    }
    return requestError(error, sentCSRF)
}

// 说明书接口统一使用同源 Cookie/CSRF；revision 放在 JSON 中，文件上传保留独立的 50 MiB 错误提示。
function createManualsApi(transport, getCSRF) {
    const client = transport || axios.create({baseURL: '/', withCredentials: true})
    function headers() {
        return browserHeaders({}, getCSRF)
    }
    async function request(config, upload = false) {
        try {
            const response = await client.request(config)
            if (!response.data || response.data.code !== 0) throw envelopeError(response)
            return response.data.data
        } catch (error) {
            throw manualRequestError(error, config.headers && config.headers['X-CSRF-Token'] || '', upload)
        }
    }
    function manualURL(manualID, suffix = '') {
        return `/api/manuals/${encodeURIComponent(String(manualID))}${suffix}`
    }
    async function listAllCategories(params = {}) {
        const baseParams = {...params}
        const startCursor = baseParams.cursor
        delete baseParams.cursor
        delete baseParams.limit
        let cursor = String(startCursor || '')
        let hasMore = true
        const seenCursors = new Set([cursor])
        const items = []
        while (hasMore) {
            const query = {...baseParams, limit: 1000}
            if (cursor) query.cursor = cursor
            const data = await request({method: 'get', url: '/api/manuals/categories', params: query, headers: headers()})
            items.push(...(data.items || []))
            hasMore = data.has_more === true
            if (!hasMore) break
            const nextCursor = String(data.next_cursor || '')
            if (!nextCursor || seenCursors.has(nextCursor)) throw new Error('分类列表分页游标未前进')
            seenCursors.add(nextCursor)
            cursor = nextCursor
        }
        return {items, has_more: false, next_cursor: ''}
    }

    return {
        listManuals(params) {
            return request({method: 'get', url: '/api/manuals', params, headers: headers()})
        },
        listCategories(params) {
            return request({method: 'get', url: '/api/manuals/categories', params, headers: headers()})
        },
        listAllCategories,
        createManual(data) {
            return request({method: 'post', url: '/api/manuals', data, headers: headers()})
        },
        getManual(manualID) {
            return request({method: 'get', url: manualURL(manualID), headers: headers()})
        },
        getPassword(manualID) {
            return request({method: 'get', url: manualURL(manualID, '/password'), headers: headers()})
        },
        setPassword(manualID, data) {
            return request({method: 'put', url: manualURL(manualID, '/password'), data, headers: headers()})
        },
        unlockManual(manualID, password) {
            return request({method: 'post', url: manualURL(manualID, '/unlock'), data: {password}, headers: headers()})
        },
        updateManual(manualID, data) {
            return request({method: 'patch', url: manualURL(manualID), data, headers: headers()})
        },
        deleteManual(manualID, revision) {
            return request({method: 'delete', url: manualURL(manualID), data: {revision}, headers: headers()})
        },
        addItem(manualID, data) {
            return request({method: 'post', url: manualURL(manualID, '/items'), data, headers: headers()})
        },
        uploadFile(manualID, file, title, clientRequestID, onUploadProgress) {
            const data = new FormData()
            data.append('file', file)
            data.append('title', title || '')
            data.append('client_request_id', clientRequestID)
            return request({
                method: 'post',
                url: manualURL(manualID, '/files'),
                data,
                headers: headers(),
                onUploadProgress,
            }, true)
        },
        deleteItem(manualID, itemID, revision) {
            return request({
                method: 'delete',
                url: manualURL(manualID, `/items/${encodeURIComponent(String(itemID))}`),
                data: {revision},
                headers: headers(),
            })
        },
    }
}

const manualsApi = createManualsApi()

module.exports = {createManualsApi, manualsApi}
