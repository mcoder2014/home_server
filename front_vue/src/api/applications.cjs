const axios = require('axios')

function envelopeError(response) {
    const body = response && response.data ? response.data : {}
    const error = new Error(body.message || body.msg || '请求失败')
    error.code = body.code
    error.status = response && response.status
    return error
}

function requestError(error) {
    if (error && error.response) {
        return envelopeError(error.response)
    }
    return error instanceof Error ? error : new Error('请求失败')
}

function createApplicationsApi(transport, getToken) {
    const client = transport || axios.create({
        baseURL: '/',
        withCredentials: true,
    })
    const tokenProvider = getToken || (() => localStorage.getItem('token') || '')

    function headers(extra) {
        return Object.assign({passport: tokenProvider()}, extra || {})
    }

    async function request(config) {
        try {
            const response = await client.request(config)
            if (!response.data || response.data.code !== 0) {
                throw envelopeError(response)
            }
            return response.data.data
        } catch (error) {
            throw requestError(error)
        }
    }

    return {
        listApplications(params) {
            return request({method: 'get', url: '/api/applications', params, headers: headers()})
        },
        createApplication(data) {
            return request({method: 'post', url: '/api/applications', data, headers: headers()})
        },
        getApplication(applicationID) {
            return request({method: 'get', url: `/api/applications/${encodeURIComponent(applicationID)}`, headers: headers()})
        },
        updateApplication(applicationID, revision, data) {
            return request({
                method: 'patch',
                url: `/api/applications/${encodeURIComponent(applicationID)}`,
                data,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        rotateSecret(applicationID, revision) {
            return request({
                method: 'post',
                url: `/api/applications/${encodeURIComponent(applicationID)}/rotate`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        revokeApplication(applicationID, revision) {
            return request({
                method: 'delete',
                url: `/api/applications/${encodeURIComponent(applicationID)}`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
    }
}

const applicationsApi = createApplicationsApi()

module.exports = {
    applicationsApi,
    createApplicationsApi,
}
