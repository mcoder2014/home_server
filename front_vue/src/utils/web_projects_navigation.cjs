const CONTROL_CHARACTER = /[\u0000-\u001f\u007f]/
const PROJECT_PATH = /^\/p\/[a-z0-9][a-z0-9-]{1,46}[a-z0-9](?:\/|$)/

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
        const parsed = new URL(value, 'https://web-projects.invalid')
        if (parsed.origin !== 'https://web-projects.invalid' || !value.startsWith('/')) {
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

function normalizeInternalRedirect(value, inheritedHash = '') {
    const parsed = parseInternalPath(value)
    if (parsed === null) {
        return '/'
    }
    if (!PROJECT_PATH.test(parsed.pathname) && parsed.pathname !== '/web-projects' && !parsed.pathname.startsWith('/web-projects/')) {
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
