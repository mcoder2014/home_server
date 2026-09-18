const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const test = require('node:test')
const vm = require('node:vm')

// Exercise the real header method; a failed logout must not pretend the Cookie was revoked.
async function runLogout(rejection) {
    const routes = [], errors = [], mutations = []
    const source = fs.readFileSync(path.join(__dirname, '../src/components/MyHeader.vue'), 'utf8')
    const script = source.match(/<script>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '').replace('export default', 'module.exports =')
    const sandbox = {UserIdentity: {}, module: {exports: {}}, require: () => ({accountsApi: {logout: async () => {if (rejection) throw rejection}}})}
    vm.runInNewContext(script, sandbox, {filename: 'MyHeader.vue'})
    const component = sandbox.module.exports
    const view = {...component.data(), $store: {commit: value => mutations.push(value)}, $router: {push: target => routes.push(target)}, $message: {error: message => errors.push(message)}}
    await component.methods.logout.call(view)
    return {routes, errors, mutations, view}
}

test('failed logout preserves the signed-in identity and page', async () => {
    const result = await runLogout(new Error('database unavailable'))
    assert.deepEqual(result.mutations, [])
    assert.deepEqual(result.routes, [])
    assert.deepEqual(result.errors, ['database unavailable'])
    assert.equal(result.view.loggingOut, false)
})

test('confirmed logout clears in-memory identity and returns home', async () => {
    const result = await runLogout()
    assert.deepEqual(result.mutations, ['REMOVE_INFO'])
    assert.deepEqual(result.routes, ['/'])
    assert.deepEqual(result.errors, [])
})

test('already expired logout clears identity and requests a new login', async () => {
    const error = new Error('expired'); error.status = 401
    const result = await runLogout(error)
    assert.deepEqual(result.mutations, ['REMOVE_INFO'])
    assert.deepEqual(result.routes, ['/login'])
})
