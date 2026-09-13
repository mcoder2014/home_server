const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const test = require('node:test')

const nginxConfig = fs.readFileSync(path.join(__dirname, '../../config/nginx/web_projects_locations.conf'), 'utf8')

test('nginx proxies primary and compatibility web-share APIs', () => {
    assert.match(nginxConfig, /location = \/api\/web-share\s*\{/)
    assert.match(nginxConfig, /location \^~ \/api\/web-share\/\s*\{/)
    assert.match(nginxConfig, /location = \/api\/web-projects\s*\{/)
    assert.match(nginxConfig, /location \^~ \/api\/web-projects\/\s*\{/)
})

test('nginx keeps raw request URI traversal guards before hosted content', () => {
    assert.match(nginxConfig, /\$request_uri/)
    assert.match(nginxConfig, /location \^~ \/p\/\s*\{/)
})
