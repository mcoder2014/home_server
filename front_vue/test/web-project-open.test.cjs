const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const test = require('node:test')
const vm = require('node:vm')

function loadComponent(api, replaced) {
    const source = fs.readFileSync(path.join(__dirname, '../src/views/WebProjectOpen.vue'), 'utf8')
    const script = source.match(/<script>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '').replace('export default', 'module.exports =')
    const navigation = require('../src/utils/web_projects_navigation.cjs')
    const sandbox = {
        Loading: {}, module: {exports: {}},
        require(name) { return name.includes('web_projects.cjs') ? {webShareApi: api} : navigation },
        window: {location: {replace: target => replaced.push(target)}},
    }
    vm.runInNewContext(script, sandbox, {filename: 'WebProjectOpen.vue'})
    return sandbox.module.exports
}

test('password-protected HEAD switches to unlock UI, unlocks by resource id and retries before navigation', async () => {
    const calls = [], replaced = []
    let unlocked = false
    const api = {
        async checkBrowserSession() { calls.push('session') },
        async probeProjectSession(target) { calls.push(`head:${target}`); if (!unlocked) { const error = new Error('locked'); error.status = 403; throw error } },
        async unlockProject(id, password) { calls.push(`unlock:${id}:${password}`); unlocked = true },
    }
    const component = loadComponent(api, replaced)
    const view = {
        ...component.data(),
        $route: {query: {target: '/p/report/', project: '9223372036854775807'}, hash: '', fullPath: '/web-share/open?target=x'},
        $store: {state: {userInfo: {id: '7'}}, commit() {}},
        $router: {replace() {}},
    }
    for (const [name, method] of Object.entries(component.methods)) view[name] = method.bind(view)

    await view.openTarget()
    assert.equal(view.passwordRequired, true)
    assert.equal(view.loading, false)
    assert.deepEqual(replaced, [])
    view.password = '家庭访问密码'
    await view.unlockProject()
    assert.deepEqual(calls, ['session', 'head:/p/report/', 'unlock:9223372036854775807:家庭访问密码', 'head:/p/report/'])
    assert.deepEqual(replaced, ['/p/report/'])
})

test('a 401 still enters login flow while a password 403 does not clear identity', async () => {
    const routes = [], commits = [], error = new Error('expired'); error.status = 401
    const component = loadComponent({async checkBrowserSession() { throw error }}, [])
    const view = {
        ...component.data(), $route: {query: {target: '/p/report/', project: '9'}, hash: '', fullPath: '/web-share/open'},
        $store: {state: {userInfo: {id: '7'}}, commit: value => commits.push(value)}, $router: {replace: value => routes.push(value)},
    }
    for (const [name, method] of Object.entries(component.methods)) view[name] = method.bind(view)
    await view.openTarget()
    assert.deepEqual(commits, ['REMOVE_INFO'])
    assert.equal(routes[0].path, '/login')
})
