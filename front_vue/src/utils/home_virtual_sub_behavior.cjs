function subscriptionInput(draft) {
    const name = String(draft.name || '')
    if (!name.trim() || new TextEncoder().encode(name).length > 256 || /[\r\n\0]/.test(name)) throw new Error('订阅名称不能为空，且不能超过 256 个 UTF-8 字节')
    const urls = String(draft.urls_text || '').split(/\r?\n/).map(value => value.trim()).filter(Boolean)
    if (!urls.length || urls.length > 64) throw new Error('请填写 1–64 个订阅地址，每行一个')
    for (const value of urls) {
        let url
        try { url = new URL(value) } catch (_) { throw new Error('订阅地址必须是完整的 HTTP 或 HTTPS 地址') }
        if (!['http:', 'https:'].includes(url.protocol) || !url.hostname) throw new Error('订阅地址必须是完整的 HTTP 或 HTTPS 地址')
    }
    const httpPort = Number(draft.http_port), socketPort = Number(draft.socket_port), outbound = Number(draft.outbound_server_count)
    if (![httpPort, socketPort].every(value => Number.isInteger(value) && value >= 1 && value <= 65535) || httpPort === socketPort) throw new Error('HTTP 和 SOCKS 端口必须不同，且在 1–65535 之间')
    if (!Number.isInteger(outbound) || outbound < 1 || outbound > 5) throw new Error('出站节点数量必须在 1–5 之间')
    const bind = String(draft.bind_ip || '').trim()
    if (!bind) throw new Error('请填写监听 IP 地址')
    return {name, urls, bind_ip: bind, http_port: httpPort, socket_port: socketPort, outbound_server_count: outbound}
}

// Each visibility change invalidates older requests. Failed reads immediately
// discard the previous online state, while the editor remains independent.
function createStatusLoader(fetchStatus, onStatus, onError) {
    let active = false, epoch = 0, pendingEpoch = -1
    return {
        activate() { if (!active) { active = true; epoch++ } },
        deactivate() { active = false; epoch++; onStatus(null) },
        invalidate() { epoch++; onStatus(null) },
        async refresh() {
            if (!active || pendingEpoch === epoch) return
            const requestEpoch = epoch
            pendingEpoch = requestEpoch
            try {
                const status = await fetchStatus()
                if (active && epoch === requestEpoch) { onStatus(status); onError(null) }
            } catch (error) {
                if (active && epoch === requestEpoch) { onStatus(null); onError(error) }
            } finally {
                if (pendingEpoch === requestEpoch) pendingEpoch = -1
            }
        },
    }
}

function workerPresentation(worker) {
    if (!worker) return {label: '状态未知', type: 'info'}
    if (worker.state === 'pending') return {label: '正在生效', type: 'warning'}
    if (worker.state === 'error') return {label: '运行失败', type: 'danger'}
    if (worker.running) return {label: '运行中', type: 'success'}
    if (worker.state === 'stopped') return {label: '已停止', type: 'info'}
    return {label: '状态未知', type: 'info'}
}

module.exports = {subscriptionInput, createStatusLoader, workerPresentation}
