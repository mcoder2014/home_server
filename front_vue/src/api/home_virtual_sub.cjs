const {createBrowserRequest} = require('./browser_client.cjs')
const prefix = '/api/admin/home-virtual-sub'

function createHomeVirtualSubApi(transport, getCSRF) {
    const request = createBrowserRequest(transport, getCSRF)
    return {
        status: () => request('get', prefix + '/status'),
        list: () => request('get', prefix + '/subscriptions'),
        detail: name => request('get', prefix + '/subscriptions/detail', undefined, undefined, {name}),
        create: (revision, data) => request('post', prefix + '/subscriptions', data, revision),
        update: (name, revision, data) => request('patch', prefix + '/subscriptions', data, revision, {name}),
        remove: (name, revision, data) => request('delete', prefix + '/subscriptions', data, revision, {name}),
        action: async (action, name, revision, data) => {
            if (!['refresh', 'start', 'stop'].includes(action)) throw new Error('未知订阅操作')
            return request('post', prefix + '/subscriptions/' + action, data, revision, {name})
        },
    }
}

const homeVirtualSubApi = createHomeVirtualSubApi()
module.exports = {createHomeVirtualSubApi, homeVirtualSubApi}
