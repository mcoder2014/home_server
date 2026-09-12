const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const test = require('node:test')
const vm = require('node:vm')

// Exercise the real header method without adding a DOM or component-test dependency.
// Only its HTTP, storage, and navigation boundaries are substituted.
async function runLogout(response, reject = false) {
    const stored = new Map([['token', 'synthetic-token'], ['user_name', 'qa-owner']])
    const routes = []
    const alerts = []
    const source = fs.readFileSync(path.join(__dirname, '../src/components/MyHeader.vue'), 'utf8')
    const script = source.match(/<script>([\s\S]*?)<\/script>/)[1]
        .replace(/^import .*$/gm, '')
        .replace('export default', 'module.exports =')
    const sandbox = {
        module: {exports: {}}, Key: {}, Monitor: {}, Reading: {},
        axios: {create: () => ({post: () => reject ? Promise.reject(response) : Promise.resolve(response)})},
        localStorage: {getItem: (key) => stored.get(key), removeItem: (key) => stored.delete(key)},
        alert: (message) => alerts.push(message),
    }
    vm.runInNewContext(script, sandbox, {filename: 'MyHeader.vue'})
    const component = sandbox.module.exports
    const view = {
        ...component.data(), hasLogin: true,
        $store: {state: {global: {baseUrl: 'https://fixture.invalid'}}},
        $router: {push: (target) => routes.push(target.path)},
    }
    component.methods.logout.call(view)
    await new Promise(setImmediate)
    return {stored, routes, alerts, view}
}

test('a failed logout envelope preserves the signed-in identity and page', async () => {
    const result = await runLogout({data: {code: 1003, message: 'database unavailable'}})
    assert.equal(result.stored.get('token'), 'synthetic-token')
    assert.equal(result.stored.get('user_name'), 'qa-owner')
    assert.equal(result.view.hasLogin, true)
    assert.deepEqual(result.routes, [])
    assert.equal(result.alerts.length, 1)
})

test('a rejected logout request preserves the signed-in identity and page', async () => {
    const result = await runLogout(new Error('network unavailable'), true)
    assert.equal(result.stored.get('token'), 'synthetic-token')
    assert.equal(result.view.hasLogin, true)
    assert.deepEqual(result.routes, [])
})

test('a confirmed logout clears local identity and returns home', async () => {
    const result = await runLogout({data: {code: 0}})
    assert.equal(result.stored.has('token'), false)
    assert.equal(result.stored.has('user_name'), false)
    assert.equal(result.view.hasLogin, false)
    assert.deepEqual(result.routes, ['/'])
    assert.deepEqual(result.alerts, [])
})
