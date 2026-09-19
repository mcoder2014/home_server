const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const test = require('node:test')

test('the initial application document suppresses referrers before a share route loads', () => {
    const html = fs.readFileSync(path.join(__dirname, '../public/index.html'), 'utf8')
    assert.match(html, /<meta\s+name=["']referrer["']\s+content=["']no-referrer["']\s*\/?\s*>/i)
})
