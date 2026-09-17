// 标签页只交换资料变化事件，身份与凭据必须通过各自 Cookie 从 /api/auth/me 重新读取。
function startAccountUpdates(host, refresh) {
    const key = 'home-server-profile-update'
    let channel = null
    const update = () => Promise.resolve(refresh()).catch(() => {})
    const message = event => { if (event.data && event.data.type === 'profile-updated') update() }
    const storage = event => { if (event.key === key && event.newValue) update() }
    const visible = () => { if (host.document.visibilityState === 'visible') update() }
    try {
        if (host.BroadcastChannel) {
            channel = new host.BroadcastChannel(key)
            channel.addEventListener('message', message)
        }
    } catch (_) { /* 浏览器禁用消息通道时使用 storage 事件。 */ }
    host.addEventListener('storage', storage)
    host.document.addEventListener('visibilitychange', visible)
    return {
        publish() {
            if (channel) channel.postMessage({type: 'profile-updated'})
            else { try { host.localStorage.setItem(key, String(Date.now()) + ':' + Math.random()) } catch (_) { /* 隐私模式下仍保留当前标签页状态。 */ } }
        },
        dispose() {
            host.removeEventListener('storage', storage)
            host.document.removeEventListener('visibilitychange', visible)
            if (channel) { channel.removeEventListener('message', message); channel.close() }
        },
    }
}

module.exports = {startAccountUpdates}
