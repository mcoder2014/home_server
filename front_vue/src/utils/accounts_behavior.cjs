// Only authenticated server responses can supply this identity. Menu visibility is not authorization.
function routeDecision(route, user) {
    const meta = route.meta || {}
    if (meta.requireAuth && (!user || user.status !== 'active')) {
        return {path: '/login', query: {redirect: route.fullPath || route.path}}
    }
    if (user && user.must_change_password && route.path !== '/account/security' && route.path !== '/login') {
        return {path: '/account/security'}
    }
    if (meta.admin && (!user || user.role !== 'admin')) {
        return {path: '/forbidden', query: {feature: 'admin'}}
    }
    if (meta.library && (!user || !user.library_enabled || (user.capabilities && user.capabilities.library === false))) {
        return {path: '/forbidden', query: {feature: 'library', reason: user && user.library_enabled ? 'disabled' : 'permission'}}
    }
    if (meta.capability && user && user.capabilities && user.capabilities[meta.capability] === false) {
        return {path: '/forbidden', query: {feature: meta.capability, reason: 'disabled'}}
    }
    return null
}

function passwordError(password, confirmation, minimum = 15) {
    minimum = Math.max(15, minimum)
    if ((password || '').includes('\u0000')) return '密码包含无效字符'
    if (Array.from(password || '').length < minimum) return `密码至少需要 ${minimum} 个字符，允许空格和粘贴`
    if (new TextEncoder().encode(password).length > 72) return '密码不能超过 72 个 UTF-8 字节（中文通常占 3 字节）'
    if (password !== confirmation) return '两次输入的密码不一致'
    return ''
}

function profilePayload(value) {
    return {display_name: value.display_name || '', contact_email: value.contact_email || '', contact_mobile: value.contact_mobile || ''}
}

function profileError(value) {
    if (!value.display_name.trim() || Array.from(value.display_name).length > 64) return '显示名称需要 1–64 个字符'
    if (value.contact_email && (value.contact_email.length > 254 || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.contact_email))) return '请填写有效的联系邮箱'
    if (value.contact_mobile && !/^[+0-9 -]{1,32}$/.test(value.contact_mobile)) return '请填写有效的联系电话'
    return ''
}

function configValues(schema, draft) {
    const values = {}
    for (const field of schema.fields || []) {
        if (field.read_only) continue
        const value = draft[field.key]
        const label = field.label || field.key
        if (field.type === 'integer') {
            if (typeof value !== 'number' || !Number.isSafeInteger(value)) throw new Error(`${label}需要填写整数`)
            if (field.minimum != null && value < field.minimum) throw new Error(`${label}不能小于 ${field.minimum}`)
            if (field.maximum != null && value > field.maximum) throw new Error(`${label}不能大于 ${field.maximum}`)
        } else if (field.type === 'boolean') {
            if (typeof value !== 'boolean') throw new Error(`${label}需要选择开启或关闭`)
        } else if (typeof value !== 'string' || (field.max_length != null && Array.from(value).length > field.max_length)) {
            throw new Error(`${label}需要填写不超过 ${field.max_length || '允许长度的'} 个字符`)
        }
        values[field.key] = value
    }
    return values
}

function configChanges(before, after) {
    return Object.keys(after).filter(key => JSON.stringify(before[key]) !== JSON.stringify(after[key]))
        .map(key => ({key, before: before[key], after: after[key]}))
}

function clearOneTimeSecret(value) {
    if (value) Object.keys(value).forEach(key => delete value[key])
    return null
}

function requestID() {
    if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID()
    const bytes = new Uint8Array(16)
    crypto.getRandomValues(bytes)
    return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
}

function dateTime(value) {
    if (!value || String(value).startsWith('0001-')) return '—'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? '—' : date.toLocaleString('zh-CN', {timeZone: 'Asia/Singapore', hour12: false})
}

module.exports = {routeDecision, passwordError, profilePayload, profileError, configValues, configChanges, clearOneTimeSecret, requestID, dateTime}
