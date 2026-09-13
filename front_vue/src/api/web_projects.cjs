const axios = require('axios')
const {browserHeaders, requestError} = require('./browser_client.cjs')

function envelopeError(response) {
    const body = response && response.data ? response.data : {}
    const error = new Error(body.message || body.msg || '请求失败')
    error.code = body.code
    error.status = response && response.status
    return error
}


// 封装网页项目和版本接口，区分 JSON 与下载流；项目变更和发布带修订号，浏览器访问使用同源 Cookie。
function createWebShareApi(transport, getCSRF) {
    const client = transport || axios.create({
        baseURL: '/',
        withCredentials: true,
    })
    function headers(extra) {
        return browserHeaders(extra, getCSRF)
    }

    async function request(config) {
        try {
            const response = await client.request(config)
            if (!response.data || response.data.code !== 0) {
                throw envelopeError(response)
            }
            return response.data.data
        } catch (error) {
            throw requestError(error, config.headers && config.headers['X-CSRF-Token'] || '')
        }
    }

    return {
        listProjects(params) {
            return request({method: 'get', url: '/api/web-share', params, headers: headers()})
        },
        createProject(data) {
            return request({method: 'post', url: '/api/web-share', data, headers: headers()})
        },
        getProject(projectID) {
            return request({method: 'get', url: `/api/web-share/${encodeURIComponent(projectID)}`, headers: headers()})
        },
        updateProject(projectID, revision, data) {
            return request({
                method: 'patch',
                url: `/api/web-share/${encodeURIComponent(projectID)}`,
                data,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        disableProject(projectID, revision) {
            return request({
                method: 'post',
                url: `/api/web-share/${encodeURIComponent(projectID)}/disable`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        deleteProject(projectID, revision) {
            return request({
                method: 'delete',
                url: `/api/web-share/${encodeURIComponent(projectID)}`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        restoreProject(projectID, revision) {
            return request({
                method: 'post',
                url: `/api/web-share/${encodeURIComponent(projectID)}/restore`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        listEligibleUsers() {
            return request({method: 'get', url: '/api/web-share/eligible-users', headers: headers()})
        },
        uploadRelease(projectID, file, entryFile) {
            const data = new FormData()
            data.append('file', file)
            if (entryFile) {
                data.append('entry_file', entryFile)
            }
            return request({
                method: 'post',
                url: `/api/web-share/${encodeURIComponent(projectID)}/releases`,
                data,
                headers: headers(),
            })
        },
        listReleases(projectID, params) {
            return request({
                method: 'get',
                url: `/api/web-share/${encodeURIComponent(projectID)}/releases`,
                params,
                headers: headers(),
            })
        },
        publishRelease(projectID, revision, releaseID) {
            return request({
                method: 'post',
                url: `/api/web-share/${encodeURIComponent(projectID)}/publish`,
                data: {release_id: releaseID},
                headers: headers({'If-Match': String(revision)}),
            })
        },
        async downloadRelease(projectID, releaseID) {
            try {
                return await client.request({
                    method: 'get',
                    url: `/api/web-share/${encodeURIComponent(projectID)}/releases/${encodeURIComponent(releaseID)}/download`,
                    responseType: 'blob',
                    headers: headers(),
                })
            } catch (error) {
                throw requestError(error, error.config && error.config.headers && error.config.headers['X-CSRF-Token'])
            }
        },
        checkBrowserSession() {
            return request({method: 'get', url: '/api/auth/me', headers: headers()})
        },
        async probeProjectSession(target) {
            try {
                await client.request({method: 'head', url: target, withCredentials: true})
                return true
            } catch (error) {
                throw requestError(error, error.config && error.config.headers && error.config.headers['X-CSRF-Token'])
            }
        },
    }
}

const webShareApi = createWebShareApi()

module.exports = {
    createWebShareApi,
    webShareApi,
}
