// URL control characters are rejected before redirects are returned to the browser.
// eslint-disable-next-line no-control-regex
const CONTROL_CHARACTER = /[\u0000-\u001f\u007f]/
const PROJECT_PATH = /^\/p\/[a-z0-9][a-z0-9-]{1,254}[a-z0-9](?:\/|$)/

// 只接受本站绝对路径，拒绝外部 origin、协议相对路径及解码后的控制符/反斜杠；非法输入返回 null。
function parseInternalPath(value) {
    if (typeof value !== 'string' || value.length === 0 || value.startsWith('//')) {
        return null
    }
    let decodedValue
    try {
        decodedValue = decodeURIComponent(value)
    } catch (error) {
        return null
    }
    if (decodedValue.includes('\\') || CONTROL_CHARACTER.test(decodedValue)) {
        return null
    }

    try {
        const parsed = new URL(value, 'https://web-share.invalid')
        if (parsed.origin !== 'https://web-share.invalid' || !value.startsWith('/')) {
            return null
        }
        return parsed
    } catch (error) {
        return null
    }
}

function isSafeProjectTarget(value) {
    const parsed = parseInternalPath(value)
    return parsed !== null && PROJECT_PATH.test(parsed.pathname)
}

// 把登录回跳限制在已知业务页面和托管内容路径，按需保留页面锚点；无法确认安全的目标回到首页。
function normalizeInternalRedirect(value, inheritedHash = '') {
    const parsed = parseInternalPath(value)
    if (parsed === null) {
        return '/'
    }
    if (!PROJECT_PATH.test(parsed.pathname)
        && !['/account', '/account/security', '/account/sessions', '/invitations', '/book/list', '/book/add', '/book/info', '/scanCodePage', '/admin/users', '/admin/web-share', '/admin/config', '/admin/audit-logs'].includes(parsed.pathname)
        && parsed.pathname !== '/applications'
        && parsed.pathname !== '/web-share'
        && !parsed.pathname.startsWith('/web-share/')
        && parsed.pathname !== '/web-projects'
        && !parsed.pathname.startsWith('/web-projects/')
        && parsed.pathname !== '/manuals'
        && !/^\/manuals\/(?:new|[1-9][0-9]*(?:\/edit)?)$/.test(parsed.pathname)) {
        return '/'
    }
    if (inheritedHash && !value.includes('#')) {
        if (typeof inheritedHash !== 'string' || !inheritedHash.startsWith('#')) {
            return '/'
        }
        const target = value + inheritedHash
        return parseInternalPath(target) === null ? '/' : target
    }
    return value
}

module.exports = {
    isSafeProjectTarget,
    normalizeInternalRedirect,
}
