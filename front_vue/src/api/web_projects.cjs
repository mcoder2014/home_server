const axios = require('axios')

async function synchronizeBrowserIdentity(api, identity, commitIdentity) {
    let browserLoginAvailable = true
    try {
        await api.createBrowserLogin(identity.token)
    } catch (error) {
        if (error.status !== 404) {
            throw error
        }
        browserLoginAvailable = false
    }
    commitIdentity(identity)
    return browserLoginAvailable
}

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

function createWebProjectsApi(transport, getToken) {
    const client = transport || axios.create({
        baseURL: '/',
        withCredentials: true,
    })
    const tokenProvider = getToken || (() => localStorage.getItem('token') || '')

    function headers(extra, token) {
        const passport = token === undefined ? tokenProvider() : token
        return Object.assign({passport}, extra || {})
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
        listProjects(params) {
            return request({method: 'get', url: '/api/web-projects', params, headers: headers()})
        },
        createProject(data) {
            return request({method: 'post', url: '/api/web-projects', data, headers: headers()})
        },
        getProject(projectID) {
            return request({method: 'get', url: `/api/web-projects/${encodeURIComponent(projectID)}`, headers: headers()})
        },
        updateProject(projectID, revision, data) {
            return request({
                method: 'patch',
                url: `/api/web-projects/${encodeURIComponent(projectID)}`,
                data,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        disableProject(projectID, revision) {
            return request({
                method: 'post',
                url: `/api/web-projects/${encodeURIComponent(projectID)}/disable`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        deleteProject(projectID, revision) {
            return request({
                method: 'delete',
                url: `/api/web-projects/${encodeURIComponent(projectID)}`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        restoreProject(projectID, revision) {
            return request({
                method: 'post',
                url: `/api/web-projects/${encodeURIComponent(projectID)}/restore`,
                headers: headers({'If-Match': String(revision)}),
            })
        },
        listEligibleUsers() {
            return request({method: 'get', url: '/api/web-projects/eligible-users', headers: headers()})
        },
        uploadRelease(projectID, file, entryFile) {
            const data = new FormData()
            data.append('file', file)
            if (entryFile) {
                data.append('entry_file', entryFile)
            }
            return request({
                method: 'post',
                url: `/api/web-projects/${encodeURIComponent(projectID)}/releases`,
                data,
                headers: headers(),
            })
        },
        listReleases(projectID, params) {
            return request({
                method: 'get',
                url: `/api/web-projects/${encodeURIComponent(projectID)}/releases`,
                params,
                headers: headers(),
            })
        },
        publishRelease(projectID, revision, releaseID) {
            return request({
                method: 'post',
                url: `/api/web-projects/${encodeURIComponent(projectID)}/publish`,
                data: {release_id: releaseID},
                headers: headers({'If-Match': String(revision)}),
            })
        },
        async downloadRelease(projectID, releaseID) {
            try {
                return await client.request({
                    method: 'get',
                    url: `/api/web-projects/${encodeURIComponent(projectID)}/releases/${encodeURIComponent(releaseID)}/download`,
                    responseType: 'blob',
                    headers: headers(),
                })
            } catch (error) {
                throw requestError(error)
            }
        },
        createBrowserLogin(token) {
            return request({method: 'post', url: '/api/web-projects/browser-login', headers: headers(null, token)})
        },
        async probeProjectSession(target) {
            try {
                await client.request({method: 'head', url: target, withCredentials: true})
                return true
            } catch (error) {
                throw requestError(error)
            }
        },
    }
}

const webProjectsApi = createWebProjectsApi()

module.exports = {
    createWebProjectsApi,
    synchronizeBrowserIdentity,
    webProjectsApi,
}
