// Exercise the actual Go-served password gate in Chromium against synthetic HTTP responses.
const {test, before, after} = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs'), path = require('node:path'), os = require('node:os')
const {chromium} = require(process.env.PLAYWRIGHT_PATH || path.join(os.homedir(), '.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright'))
const source = fs.readFileSync(path.join(__dirname, '../../api/webprojects/content.go'), 'utf8')
const html = source.match(/html := `([\s\S]*?)`\n/)[1].replace('` + endpoint + `', '/api/web-share/42/unlock')
let browser
before(async () => { browser = await chromium.launch({headless: true, ...(process.platform === 'darwin' ? {executablePath: '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'} : {})}) })
after(async () => { if (browser) await browser.close() })
for (const code of ['1234', 'wrong', '']) {
    test(`query ${code || '(empty)'} is removed before POST and permits manual correction`, async () => {
        const page = await browser.newPage()
        let unlocked = false
        const requests = []
        await page.route('https://home.example.com/**', async route => {
            const request = route.request(), url = new URL(request.url())
            if (url.pathname.endsWith('/unlock')) {
                requests.push({url: page.url(), referrer: request.headers().referer, body: request.postDataJSON()})
                unlocked = request.postDataJSON().password === '1234'
                return route.fulfill({status: unlocked ? 200 : 403, contentType: 'application/json', body: '{}'})
            }
            return route.fulfill({status: unlocked ? 200 : 403, contentType: 'text/html', headers: {'Referrer-Policy': 'no-referrer', 'Cache-Control': 'no-store'}, body: unlocked ? '<h1>Protected content</h1>' : html})
        })
        await page.goto(`https://home.example.com/p/report/?view=wide&code=${code}&code=discard#section`)
        if (code !== '1234') {
            await page.getByRole('alert').filter({hasText: '密码不正确'}).waitFor()
            assert.equal(page.url(), 'https://home.example.com/p/report/?view=wide#section')
            await page.locator('#password').fill('1234')
            await page.getByRole('button', {name: '解锁网页'}).click()
        }
        await page.getByRole('heading', {name: 'Protected content'}).waitFor()
        assert.equal(page.url(), 'https://home.example.com/p/report/?view=wide#section')
        assert.equal(requests.length, code === '1234' ? 1 : 2)
        assert.equal(requests[0].body.password, code)
        for (const request of requests) { assert(!request.url.includes('code=')); assert.equal(request.referrer, undefined) }
        await page.close()
    })
}

test('unavailable page keeps 404 while removing code before its home link can forward a referrer', async () => {
    const source = fs.readFileSync(path.join(__dirname, '../../api/webprojects/container.go'), 'utf8')
    const notFound = source.match(/`(<!doctype html><html lang="zh-CN"><meta charset="utf-8"><title>404[\s\S]*?)`/)[1]
    const page = await browser.newPage()
    await page.route('https://home.example.com/**', route => route.fulfill({status: 404, contentType: 'text/html', headers: {'Referrer-Policy': 'no-referrer'}, body: notFound}))
    const response = await page.goto('https://home.example.com/p/private/?code=1234&view=wide#section')
    assert.equal(response.status(), 404)
    assert.equal(page.url(), 'https://home.example.com/p/private/?view=wide#section')
    await page.getByRole('heading', {name: '404'}).waitFor()
    await page.close()
})
