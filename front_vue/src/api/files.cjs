const axios = require('axios')
const {browserHeaders, requestError} = require('./browser_client.cjs')

function envelopeError(response) {
    const body = response && response.data || {}
    const error = new Error(body.message || body.msg || '请求失败')
    error.code = body.code
    error.status = response && response.status
    return error
}

async function downloadRequestError(error, sentCSRF) {
    const response = error && error.response
    if (response && typeof Blob !== 'undefined' && response.data instanceof Blob) {
        try {
            const parsed = JSON.parse(await response.data.text())
            error.response = {...response, data: parsed}
        } catch (_) { /* 非 JSON 下载错误继续使用通用提示。 */ }
    }
    return requestError(error, sentCSRF)
}

function createFilesApi(transport, getCSRF) {
    const client = transport || axios.create({baseURL: '/', withCredentials: true})
    const headers = () => browserHeaders({}, getCSRF)
    async function request(config, upload = false) {
        try {
            const response = await client.request(config)
            if (!response.data || response.data.code !== 0) throw envelopeError(response)
            return response.data.data
        } catch (error) {
            if (upload && error && error.response && error.response.status === 413) {
                const result = new Error('文件上传失败：单个文件不能超过 50 MiB')
                result.code = error.response.data && error.response.data.code
                result.status = 413
                throw result
            }
            throw requestError(error, config.headers && config.headers['X-CSRF-Token'] || '')
        }
    }
    const fileURL = id => `/api/files/${encodeURIComponent(String(id))}`
    const publicURL = token => `/api/file-shares/${encodeURIComponent(String(token))}`
    return {
        listFiles(params) { return request({method: 'get', url: '/api/files', params, headers: headers()}) },
        listEligibleUsers() { return request({method: 'get', url: '/api/files/eligible-users', headers: headers()}) },
        uploadFile(file, onUploadProgress) {
            const data = new FormData()
            data.append('file', file)
            return request({method: 'post', url: '/api/files', data, headers: headers(), onUploadProgress}, true)
        },
        getFile(id) { return request({method: 'get', url: fileURL(id), headers: headers()}) },
        deleteFile(id) { return request({method: 'delete', url: fileURL(id), headers: headers()}) },
        listShares(id, params) { return request({method: 'get', url: `${fileURL(id)}/shares`, params, headers: headers()}) },
        createShare(id, data) { return request({method: 'post', url: `${fileURL(id)}/shares`, data, headers: headers()}) },
        revokeShare(id, shareID) { return request({method: 'delete', url: `${fileURL(id)}/shares/${encodeURIComponent(String(shareID))}`, headers: headers()}) },
        getShare(token) { return request({method: 'get', url: publicURL(token), headers: headers()}) },
        unlockShare(token, secret) { return request({method: 'post', url: `${publicURL(token)}/unlock`, data: {secret}, headers: headers()}) },
        async downloadShare(token) {
            const config = {method: 'post', url: `${publicURL(token)}/download`, data: null, responseType: 'blob', headers: headers()}
            try { return await client.request(config) }
            catch (error) { throw await downloadRequestError(error, config.headers['X-CSRF-Token'] || '') }
        },
    }
}

const filesApi = createFilesApi()
module.exports = {createFilesApi, filesApi}
