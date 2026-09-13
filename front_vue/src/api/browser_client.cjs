const axios = require('axios')
let csrfProvider = () => ''
let identityFailure = () => {}

function configureBrowserSession(getCSRF, onIdentityFailure) {
    csrfProvider = getCSRF
    identityFailure = onIdentityFailure
}

function browserHeaders(extra, getCSRF = csrfProvider) {
    const csrf = getCSRF()
    return Object.assign(csrf ? {'X-CSRF-Token': csrf} : {}, extra || {})
}

function responseError(response) {
    const body = response && response.data || {}
    const error = new Error(body.message || body.msg || '请求失败，请稍后重试')
    error.code = body.code
    error.status = response && response.status
    return error
}

function requestError(error, sentCSRF) {
    const normalized = error && error.response ? responseError(error.response) : error
    if (normalized && normalized.status === 401 && (sentCSRF === undefined || sentCSRF === csrfProvider())) identityFailure()
    return normalized instanceof Error ? normalized : new Error('网络连接失败，请稍后重试')
}

function createBrowserRequest(transport, getCSRF) {
    const client = transport || axios.create({baseURL: '/', withCredentials: true})
    return async function request(method, url, data, revision, params) {
        const headers = browserHeaders(revision == null ? {} : {'If-Match': String(revision)}, getCSRF)
        try {
            const response = await client.request({method, url, data, params, headers, withCredentials: true})
            if (!response.data || response.data.code !== 0) throw responseError(response)
            return response.data.data
        } catch (error) {
            throw requestError(error, headers['X-CSRF-Token'] || '')
        }
    }
}

module.exports = {configureBrowserSession, browserHeaders, responseError, requestError, createBrowserRequest}
