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

function profilePayload(value, before) {
    const fields = {display_name: (value.display_name || '').trim(), contact_email: value.contact_email || '', contact_mobile: value.contact_mobile || ''}
    if (before) {
        const saved = profilePayload(before)
        for (const key of Object.keys(fields)) if (fields[key] === saved[key]) delete fields[key]
    }
    return fields
}

function profileError(value) {
    if (Array.from((value.display_name || '').trim()).length > 64) return '昵称不能超过 64 个字符'
    if (value.contact_email && (value.contact_email.length > 254 || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value.contact_email))) return '请填写有效的联系邮箱'
    if (value.contact_mobile && !/^[+0-9 -]{1,32}$/.test(value.contact_mobile)) return '请填写有效的联系电话'
    return ''
}

function userIdentity(value = {}) {
    const clean = text => String(text || '').replace(/[\p{Cc}\p{Cf}]/gu, character => character === '\u200d' ? character : '').trim()
    const name = clean(value.display_name) || clean(value.user_name) || (value.user_id || value.id ? '用户 ' + (value.user_id || value.id) : '用户')
    const visible = clean(value.display_name) || clean(value.user_name)
    let initial = Array.from(visible)[0] || ''
    if (visible && typeof Intl.Segmenter === 'function') initial = new Intl.Segmenter('zh', {granularity: 'grapheme'}).segment(visible)[Symbol.iterator]().next().value.segment
    const avatar = /^\/api\/account\/avatars\/[1-9][0-9]*\/[1-9][0-9]*$/.test(value.avatar_url || '') ? value.avatar_url : ''
    return {name, initial: initial.toUpperCase(), avatar}
}

function preserveProfileDraft(draft, before, latest) {
    const next = profilePayload(latest)
    for (const key of Object.keys(profilePayload(draft, before))) next[key] = draft[key]
    return next
}

function selectedSessionIDs(selected, items, current) {
    const available = new Set(items.map(item => String(item.id)))
    const currentID = current && String(current.id)
    return [...new Set(selected.map(String))].filter(id => id !== currentID && available.has(id)).slice(0, 100)
}

function sessionRemaining(session, receivedAt, now) {
    const seconds = Number(session.remaining_seconds)
    if (!Number.isFinite(seconds)) return null
    return Math.max(0, Math.ceil(seconds - Math.max(0, now - receivedAt) / 1000))
}

function remainingText(seconds) {
    if (seconds == null) return '剩余时间未知'
    if (seconds <= 0) return '已到期，等待刷新确认'
    const days = Math.floor(seconds / 86400), hours = Math.floor(seconds % 86400 / 3600), minutes = Math.floor(seconds % 3600 / 60)
    if (days) return `剩余 ${days} 天 ${hours} 小时`
    if (hours) return `剩余 ${hours} 小时 ${minutes} 分钟`
    return seconds < 60 ? '剩余不足 1 分钟' : `剩余 ${minutes} 分钟`
}

function avatarFileError(file) {
    if (!file || !['image/jpeg', 'image/png'].includes(file.type)) return '头像只支持 JPEG 或 PNG 图片'
    if (!file.size || file.size > 2 * 1024 * 1024) return '头像文件必须非空，且不能超过 2 MiB'
    return ''
}

// 读取有界文件头，先拒绝像素炸弹和 APNG，再交给浏览器解码；实际图片仍由服务端独立校验。
function avatarMetadata(input) {
    const bytes = input instanceof Uint8Array ? input : new Uint8Array(input)
    const data = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength)
    let width = 0, height = 0, orientation = 1, type = ''
    if (bytes.length >= 33 && [137, 80, 78, 71, 13, 10, 26, 10].every((value, index) => bytes[index] === value)) {
        type = 'image/png'
        if (data.getUint32(8) !== 13 || String.fromCharCode(...bytes.slice(12, 16)) !== 'IHDR') throw new Error('PNG 文件头无效')
        width = data.getUint32(16); height = data.getUint32(20)
        for (let offset = 8; offset + 12 <= bytes.length;) {
            const length = data.getUint32(offset), chunk = String.fromCharCode(...bytes.slice(offset + 4, offset + 8))
            if (chunk === 'acTL') throw new Error('头像不支持动画 PNG')
            if (length > bytes.length - offset - 12) throw new Error('PNG 图片内容不完整')
            offset += length + 12
        }
    } else if (bytes.length > 4 && bytes[0] === 255 && bytes[1] === 216) {
        type = 'image/jpeg'
        for (let offset = 2; offset + 4 <= bytes.length;) {
            if (bytes[offset] !== 255) throw new Error('JPEG 图片内容无效')
            while (bytes[offset] === 255) offset++
            const marker = bytes[offset++]
            if (marker === 217 || marker === 218) break
            if (marker === 1 || marker >= 208 && marker <= 215) continue
            const length = data.getUint16(offset)
            if (length < 2 || offset + length > bytes.length) throw new Error('JPEG 图片内容不完整')
            if ([192, 193, 194, 195, 197, 198, 199, 201, 202, 203, 205, 206, 207].includes(marker) && length >= 8) {
                height = data.getUint16(offset + 3); width = data.getUint16(offset + 5)
            }
            if (marker === 225) orientation = avatarOrientation(bytes, offset + 2, length - 2)
            offset += length
        }
    }
    if (!width || !height) throw new Error('无法识别 JPEG 或 PNG 图片尺寸')
    if (width > 4096 || height > 4096) throw new Error('图片宽高均不能超过 4096 像素')
    if (width * height > 16000000) throw new Error('图片总像素不能超过 1600 万')
    return {width, height, orientation, type}
}

function avatarOrientation(bytes, start, length) {
    if (length < 14 || String.fromCharCode(...bytes.slice(start, start + 6)) !== 'Exif\u0000\u0000') return 1
    const data = new DataView(bytes.buffer, bytes.byteOffset + start + 6, length - 6)
    const endian = data.getUint16(0), little = endian === 0x4949
    if ((!little && endian !== 0x4d4d) || data.getUint16(2, little) !== 42) return 1
    const directory = data.getUint32(4, little)
    if (directory + 2 > data.byteLength) return 1
    const count = data.getUint16(directory, little)
    for (let index = 0; index < count; index++) {
        const offset = directory + 2 + index * 12
        if (offset + 12 > data.byteLength) break
        if (data.getUint16(offset, little) === 0x112 && data.getUint16(offset + 2, little) === 3 && data.getUint32(offset + 4, little) === 1) {
            const orientation = data.getUint16(offset + 8, little)
            return orientation >= 1 && orientation <= 8 ? orientation : 1
        }
    }
    return 1
}

function avatarCrop(width, height, zoom, offsetX, offsetY, stage = 280) {
    const scale = stage / Math.min(width, height) * Math.max(1, Math.min(3, zoom))
    const boundX = (width * scale - stage) / 2, boundY = (height * scale - stage) / 2
    offsetX = Math.max(-boundX, Math.min(boundX, offsetX)) || 0; offsetY = Math.max(-boundY, Math.min(boundY, offsetY)) || 0
    return {x: (boundX - offsetX) / scale, y: (boundY - offsetY) / scale, size: stage / scale, offsetX, offsetY, scale}
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

module.exports = {routeDecision, passwordError, profilePayload, profileError, userIdentity, preserveProfileDraft, selectedSessionIDs, sessionRemaining, remainingText, avatarFileError, avatarMetadata, avatarCrop, configValues, configChanges, clearOneTimeSecret, requestID, dateTime}
