const qualityText = {
    ok: '采集正常', unknown: '数据缺口', degraded: '数据不完整', not_started: '尚未开始统计', disabled: '统计已关闭',
}

function shanghaiDate(value) {
    const date = new Date(value)
    if (!Number.isFinite(date.getTime())) return ''
    const parts = new Intl.DateTimeFormat('en-CA', {
        timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit',
    }).formatToParts(date)
    const fields = Object.fromEntries(parts.map(part => [part.type, part.value]))
    return `${fields.year}-${fields.month}-${fields.day}`
}

function count(value) {
    // 空值或不安全的数值不表示零流量。
    if (typeof value !== 'number' || !Number.isSafeInteger(value) || value < 0) return null
    return value
}

// 将服务端每日快照映射到固定日期轴；只连接健康数据，未知/部分采集日保留缺口。
// 累计 UV 始终来自独立历史值，不从每日 UV 相加；缺失日期也不能由客户端推断为零。
function buildStatsView(stats, days = 30, now = new Date()) {
    const todayDate = shanghaiDate(now)
    const startedDate = stats.tracking_started_at ? shanghaiDate(stats.tracking_started_at) : ''
    const quality = stats.enabled === false ? 'disabled' : (qualityText[stats.quality] ? stats.quality : 'unknown')
    const rows = new Map((stats.daily || []).map(day => [day.date, day]))
    const end = Date.parse(`${todayDate}T00:00:00Z`)
    const daily = Array.from({length: days}, (_, index) => {
        const date = new Date(end - (days - index - 1) * 86400000).toISOString().slice(0, 10)
        const row = rows.get(date)
        let dayQuality = row && qualityText[row.quality] ? row.quality : 'unknown'
        if (!row && (!startedDate || date < startedDate)) dayQuality = 'not_started'
        if (quality === 'disabled' && date === todayDate) dayQuality = 'disabled'
        let pv = row && count(row.pv), uv = row && count(row.uv)
        if (dayQuality === 'ok' && (pv == null || uv == null)) dayQuality = 'unknown'
        if (!['ok', 'degraded'].includes(dayQuality)) { pv = null; uv = null }
        return {date, pv: pv ?? null, uv: uv ?? null, quality: dayQuality,
            quality_reason: row && row.quality_reason || '', x: 52 + index * 828 / (days - 1)}
    })
    const peak = Math.max(1, ...daily.filter(day => day.quality === 'ok').flatMap(day => [day.pv, day.uv]))
    const paths = {pv: [], uv: []}
    for (const metric of ['pv', 'uv']) {
        let segment = []
        for (const day of daily) {
            if (day.quality === 'ok') {
                day[`${metric}Y`] = 216 - day[metric] / peak * 184
                segment.push(`${segment.length ? 'L' : 'M'}${day.x.toFixed(2)},${day[`${metric}Y`].toFixed(2)}`)
            } else if (segment.length) {
                paths[metric].push(segment.join(' '))
                segment = []
            }
        }
        if (segment.length) paths[metric].push(segment.join(' '))
    }
    const knownDays = daily.filter(day => day.pv != null)
    return {
        quality, daily, today: daily[daily.length - 1], paths, peak,
        total: {pv: stats.persisted_at ? count(stats.total && stats.total.pv) : null,
            uv: stats.persisted_at ? count(stats.total && stats.total.uv) : null},
        periodPV: knownDays.length ? knownDays.reduce((total, day) => total + day.pv, 0) : null,
        periodPartial: daily.some(day => ['unknown', 'degraded', 'disabled'].includes(day.quality)),
        ticks: [...new Set([0, Math.round((days - 1) / 2), days - 1])].map(index => daily[index]),
    }
}

module.exports = {buildStatsView, qualityText}
