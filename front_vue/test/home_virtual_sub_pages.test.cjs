const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const vm = require('node:vm')
const path = require('node:path')

function component(api) {
    const source = fs.readFileSync(path.join(__dirname, '../src/views/AdminHomeVirtualSub.vue'), 'utf8')
    const script = source.match(/<script>([\s\S]*?)<\/script>/)[1].replace(/^import .*$/gm, '').replace('export default', 'module.exports =')
    const box = {module: {exports: {}}, AdminConfirm: {}, TextEncoder, URL, setInterval, clearInterval,
        require: name => name === '@/api/home_virtual_sub.cjs' ? {homeVirtualSubApi: api} : require(path.join(__dirname, '../src/', name.slice(2)))}
    vm.runInNewContext(script, box)
    const definition = box.module.exports, view = {}
    Object.assign(view, definition.data.call(view))
    for (const [name, method] of Object.entries(definition.methods)) view[name] = method.bind(view)
    for (const [name, getter] of Object.entries(definition.computed)) Object.defineProperty(view, name, {get: () => getter.call(view)})
    view.status = {enabled: true, workers: []}
    view.refreshStatus = async () => {}
    view.loadList = async () => {}
    return view
}

test('conflict keeps the original draft and cannot automatically resend', async () => {
    let calls = 0
    const view = component({update: async () => {calls++; throw Object.assign(new Error('配置已变化'), {status: 409})}})
    view.draft = {name: 'demo', urls_text: 'https://example.com/private'}
    view.draftRevision = 'a'.repeat(64)
    view.pendingAction = {action: 'update', name: 'demo', revision: view.draftRevision, input: {urls: ['https://example.com/private']}}
    view.confirmVisible = true
    await view.submit({reason: '测试', current_password: 'synthetic'})
    assert.equal(calls, 1)
    assert.equal(view.draft.urls_text, 'https://example.com/private')
    assert.equal(view.draftRevision, 'a'.repeat(64))
    assert.equal(view.conflict, true)
    assert.equal(view.pendingAction, null)
    assert.equal(view.confirmVisible, false)
})

test('late create reload cannot replace the revision belonging to a newer edit', async () => {
    let resolveList
    const view = component({detail: async () => ({revision: 'a'.repeat(64), subscription: {name: 'existing', urls: ['https://example.com/a']}})})
    view.editing = false
    view.editorVisible = true
    view.loadList = () => new Promise(resolve => {resolveList = () => {view.revision = 'b'.repeat(64); resolve(true)}})
    const reload = view.reloadDraft()
    view.editorVisible = false
    view.clearDraft()
    await view.openEdit({name: 'existing'})
    resolveList()
    await reload
    assert.equal(view.draftRevision, 'a'.repeat(64))
    assert.equal(view.draft.name, 'existing')
})

test('a pre-mutation status request cannot restore online after an ambiguous write failure', async () => {
    const {createStatusLoader} = require('../src/utils/home_virtual_sub_behavior.cjs')
    let resolveStatus
    const view = component({remove: async () => {throw Object.assign(new Error('结果未知'), {status: 503})}})
    view.loader = createStatusLoader(() => new Promise(resolve => {resolveStatus = resolve}), value => {view.status = value}, () => {})
    view.loader.activate()
    const oldRead = view.loader.refresh()
    view.pendingAction = {action: 'delete', name: 'demo', revision: 'a'.repeat(64), input: {}}
    await view.submit({reason: '测试', current_password: 'synthetic'})
    resolveStatus({enabled: true, running: 1, revision: 'a'.repeat(64)})
    await oldRead
    assert.equal(view.status, null)
    assert.equal(view.canManage, false)
})

test('ambiguous failure clears online status and prevents blind repeated submission', async () => {
    let calls = 0
    const view = component({remove: async () => {calls++; throw Object.assign(new Error('操作结果未知，请核对'), {status: 503})}})
    view.pendingAction = {action: 'delete', name: 'demo', revision: 'a'.repeat(64), input: {}}
    await view.submit({reason: '测试', current_password: 'synthetic'})
    assert.equal(calls, 1)
    assert.equal(view.status, null)
    assert.equal(view.pendingAction, null)
    assert.equal(view.mutationUncertain, true)
    await view.submit({reason: '重复', current_password: 'synthetic'})
    assert.equal(calls, 1)
})

test('closing editor clears sensitive URLs and readonly advanced state', () => {
    const view = component({})
    view.draft = {name: 'demo', urls_text: 'https://example.com/private?token=synthetic'}
    view.hasProxyChain = true
    view.draftRevision = 'a'.repeat(64)
    view.clearDraft()
    assert.equal(view.draft.urls_text, '')
    assert.equal(view.draftRevision, '')
    assert.equal(view.hasProxyChain, false)
})

test('late edit detail cannot reopen a closed editor or overwrite a newer draft', async () => {
    const pending = []
    const view = component({detail: name => new Promise(resolve => pending.push({name, resolve}))})
    view.editorVisible = true
    const first = view.openEdit({name: 'first'})
    view.editorVisible = false
    view.clearDraft()
    pending[0].resolve({revision: 'a'.repeat(64), subscription: {name: 'first', urls: ['https://example.com/private']}})
    await first
    assert.equal(view.editorVisible, false)
    assert.equal(view.draft.urls_text, '')
    const second = view.openEdit({name: 'second'})
    const third = view.openEdit({name: 'third'})
    pending[2].resolve({revision: 'b'.repeat(64), subscription: {name: 'third', urls: ['https://example.com/third']}})
    await third
    pending[1].resolve({revision: 'a'.repeat(64), subscription: {name: 'second', urls: ['https://example.com/second']}})
    await second
    assert.equal(view.draft.name, 'third')
    assert.equal(view.draftRevision, 'b'.repeat(64))
})
