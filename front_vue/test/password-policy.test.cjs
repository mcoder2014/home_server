const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const vm = require('node:vm')
const {passwordError} = require('../src/utils/accounts_behavior.cjs')
const {secretError, resourcePasswordError, randomShareCode} = require('../src/utils/file_sharing_behavior.cjs')

test('configured four-character account and sharing secrets retain upper bounds and confirmation', () => {
    assert.equal(passwordError('1234', '1234', 4), '')
    assert.match(passwordError('123', '123', 4), /4/)
    assert.match(passwordError('1234', '4321', 4), /不一致/)
    assert.match(passwordError('😀'.repeat(19), '😀'.repeat(19), 4), /72/)
    assert.equal(secretError('password', '1234', 4), '')
    assert.equal(resourcePasswordError('1234', '1234', 4), '')
    assert.match(resourcePasswordError('123', '123', 4), /4/)
    assert.equal(secretError('code', 'aB12', 4, 4), '')
    assert.match(secretError('code', 'aB1234', 4, 4), /4 位/)
    for (const length of [4, 6, 12]) assert.equal(randomShareCode(a => a.fill(1), length).length, length)
    assert.match(resourcePasswordError('1234', '1234'), /8/)
})

// Execute both scripts from the actual Go-served gate in document order.
function gate(query, responseStatus = 200) {
    const source = fs.readFileSync(path.join(__dirname, '../../api/webprojects/content.go'), 'utf8')
    const html = source.match(/html := `([\s\S]*?)`\n/)[1].replace('` + endpoint + `', '/api/web-share/42/unlock')
    const scripts = Array.from(html.matchAll(/<script>([\s\S]*?)<\/script>/g), match => match[1])
    let href = 'https://home.example.com/p/report/index.html' + query
    const requests = [], redirects = [], nodes = {unlock: {addEventListener(_, fn) { this.submit = fn }}, password: {value: ''}, submit: {}, error: {}}
    const context = vm.createContext({URL, document: {getElementById: id => nodes[id]},
        history: {state: {keep: true}, replaceState(state, _, value) { assert.equal(state.keep, true); href = new URL(value, href).href }},
        location: {get href() { return href }, replace(value) { redirects.push(value) }},
        fetch: async (url, options) => { requests.push({url, options, href}); return {ok: responseStatus === 200, status: responseStatus} },
    })
    scripts.forEach(script => vm.runInContext(script, context))
    return {requests, redirects, nodes, get href() { return href }}
}

test('query code is cleared before POST; unrelated query and fragment survive success', async () => {
    const page = gate('?view=wide&code=1234&code=discard#section')
    assert.equal(page.requests.length, 1)
    assert.equal(page.requests[0].href, 'https://home.example.com/p/report/index.html?view=wide#section')
    assert.equal(page.requests[0].options.method, 'POST')
    assert.equal(JSON.parse(page.requests[0].options.body).password, '1234')
    await new Promise(resolve => setImmediate(resolve))
    assert.deepEqual(page.redirects, ['/p/report/index.html?view=wide#section'])
})

test('wrong or empty code stays on clean URL and supports a manual retry without auto-loop', async () => {
    for (const query of ['?code=wrong', '?code=']) {
        const page = gate(query, 403)
        await new Promise(resolve => setImmediate(resolve))
        assert.equal(new URL(page.href).search, '')
        assert.equal(page.nodes.error.textContent, '密码不正确')
        assert.equal(page.nodes.submit.disabled, false)
        assert.equal(page.redirects.length, 0)
        page.nodes.password.value = '1234'
        page.nodes.unlock.submit({preventDefault() {}})
        assert.equal(page.requests.length, 2)
    }
})

test('no code leaves manual form idle; throttling differs from incorrect password', async () => {
    assert.equal(gate('?view=wide').requests.length, 0)
    const page = gate('?code=1234', 429)
    await new Promise(resolve => setImmediate(resolve))
    assert.match(page.nodes.error.textContent, /频繁/)
})
