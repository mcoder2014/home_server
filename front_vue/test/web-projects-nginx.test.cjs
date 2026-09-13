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

const authConfig = fs.readFileSync(path.join(__dirname, '../../config/nginx/api_auth_locations.conf'), 'utf8')
const frontendConfig = fs.readFileSync(path.join(__dirname, '../../deploy/pi/frontend-nginx.conf'), 'utf8')

// Read the flat location blocks used by these repository snippets without
// starting a server or applying deployment configuration.
function locationBody(config, selector) {
    const start = config.indexOf(`${selector} {`)
    assert.notEqual(start, -1, `missing ${selector}`)
    const open = config.indexOf('{', start)
    const close = config.indexOf('}', open)
    assert.notEqual(close, -1, `unclosed ${selector}`)
    return config.slice(open + 1, close)
}

// Verify the new account namespaces reach the backend ahead of the frontend's
// /api/ rejection, and retain bounded bodies plus trusted forwarding metadata.
test('nginx forwards site account and admin APIs with bounded uncached requests', () => {
    for (const [namespace, limit] of [['site', '16k'], ['account', '16k'], ['admin', '64k']]) {
        const body = locationBody(authConfig, `location ^~ /api/${namespace}/`)
        assert.match(body, new RegExp(`client_max_body_size ${limit};`))
        assert.match(body, /proxy_pass http:\/\/127\.0\.0\.1:18080;/)
        assert.match(body, /proxy_set_header Host \$http_host;/)
        assert.match(body, /proxy_set_header X-Forwarded-Proto \$scheme;/)
        assert.match(body, /proxy_set_header X-Forwarded-For \$remote_addr;/)
        assert.match(body, /access_log off;/)
        assert.match(body, /proxy_cache off;/)
        assert.match(body, /proxy_intercept_errors off;/)
        assert.match(body, /add_header Cache-Control "no-store" always;/)
    }
    assert.match(frontendConfig, /include \/etc\/home_server\/locations\/api_auth_locations\.conf;/)
    assert.match(frontendConfig, /location \/api\/ \{ return 404; \}/)
    assert.match(frontendConfig, /"192\.0\.2\.20:https" https;/)
    assert.match(frontendConfig, /default \$remote_addr;/)
})

// Both documented TLS gateways must match only the new API namespaces, retain
// their original port/upstream and preserve host ports for exact Origin checks.
test('both gateway templates preserve controlled API matching and existing origins', () => {
    const selector = 'location ~ ^/api/(site|account|admin)/'
    const route = new RegExp('^/api/(site|account|admin)/')
    for (const uri of ['/api/site/bootstrap', '/api/account/profile', '/api/admin/config/site', '/api/admin/web-share/12/releases/3/preview/assets/app.js']) {
        assert.equal(route.test(uri), true)
    }
    for (const uri of ['/api/administrator/users', '/api/accounting/profile', '/api/site-evil/bootstrap', '/api/web-share/12/releases', '/webdav/file']) {
        assert.equal(route.test(uri), false)
    }
    for (const [file, port, upstream] of [['gateway-frontend.conf', 8080, 18081], ['gateway-api.conf', 18080, 18080]]) {
        const config = fs.readFileSync(path.join(__dirname, '../../deploy/pi', file), 'utf8')
        const body = locationBody(config, selector)
        assert.match(config, new RegExp(`listen ${port} ssl;`))
        assert.match(config, /server_name home\.example\.com home\.internal\.example\.com;/)
        assert.match(body, new RegExp(`proxy_pass http://192\\.0\\.2\\.10:${upstream};`))
        assert.match(body, /client_max_body_size 64k;/)
        assert.match(body, /proxy_set_header Host \$http_host;/)
        assert.match(body, /proxy_set_header X-Forwarded-Proto \$scheme;/)
        assert.match(body, /proxy_set_header X-Forwarded-For \$remote_addr;/)
        assert.match(body, /proxy_cache off;/)
        assert.match(body, /proxy_intercept_errors off;/)
        assert.match(body, /add_header Cache-Control "no-store" always;/)
    }
})

// Account API routing must not consume WebDAV paths or rewrite its Basic
// authorization; the existing large-body streaming proxy remains independent.
test('account proxy additions preserve the existing WebDAV transport', () => {
    const body = locationBody(authConfig, 'location ~ ^/(library|bookinfo|webdav|webdav_dev)/')
    assert.match(body, /client_max_body_size 409600m;/)
    assert.match(body, /proxy_request_buffering off;/)
    assert.match(body, /proxy_set_header Host \$http_host;/)
    assert.doesNotMatch(body, /proxy_set_header Authorization|auth_basic|limit_except/)
})
