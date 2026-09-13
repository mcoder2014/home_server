const assert = require('node:assert/strict')
const test = require('node:test')
const {createAccountsApi} = require('../src/api/accounts.cjs')
const {configureBrowserSession} = require('../src/api/browser_client.cjs')

test('a late unauthorized response from an older session cannot sign out a newly logged-in user', async () => {
    let csrf = 'old-session', cleared = 0, reject
    configureBrowserSession(() => csrf, () => {cleared++})
    const api = createAccountsApi({request: () => new Promise((resolve, fail) => {reject = fail})})
    const pending = api.me()
    csrf = 'new-session'
    const error = new Error('expired'); error.response = {status: 401, data: {code: 40101, message: 'old session expired'}}
    reject(error)
    await assert.rejects(pending, error => error.status === 401)
    assert.equal(cleared, 0)
    configureBrowserSession(() => '', () => {})
})

test('an unauthorized response for the current session still invalidates browser identity', async () => {
    let cleared = 0
    configureBrowserSession(() => 'current-session', () => {cleared++})
    const api = createAccountsApi({async request() {const error = new Error(); error.response = {status: 401, data: {code: 40101}}; throw error}})
    await assert.rejects(api.me())
    assert.equal(cleared, 1)
    configureBrowserSession(() => '', () => {})
})

function loadStore(api) {
    const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm')
    const source = fs.readFileSync(path.join(__dirname, '../src/store/index.js'), 'utf8').replace(/^import .*$/gm, '').replace('export default store', 'module.exports = store')
    const sandbox = {module: {exports: {}}, config: {serverUrl: '/'}, localStorage: {removeItem() {}}, sessionStorage: {removeItem() {}},
        require: name => name.includes('accounts.cjs') ? {accountsApi: api} : {configureBrowserSession() {}},
        createStore(definition) {
            const store = {state: definition.state}
            store.commit = (name, value) => definition.mutations[name](store.state, value)
            store.dispatch = name => definition.actions[name]({commit: store.commit, state: store.state})
            return store
        },
    }
    vm.runInNewContext(source, sandbox)
    return sandbox.module.exports
}

test('a delayed me response cannot replace a newer login identity', async () => {
    let finish
    const store = loadStore({me: () => new Promise(resolve => {finish = resolve})})
    const pending = store.dispatch('refreshSession')
    store.commit('SET_USERINFO', {id: '200', role: 'user', csrf_token: 'new'})
    finish({id: '100', role: 'admin', csrf_token: 'old'})
    await pending
    assert.equal(store.state.userInfo.id, '200')
})

test('a delayed me response cannot resurrect identity after an explicit logout', async () => {
    let finish
    const store = loadStore({me: () => new Promise(resolve => {finish = resolve})})
    const pending = store.dispatch('refreshSession')
    store.commit('REMOVE_INFO')
    finish({id: '100', role: 'admin'})
    await pending
    assert.equal(store.state.userInfo, null)
})
