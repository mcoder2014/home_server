/* Home Server 网页评论容器。只在自己的 Shadow DOM 中渲染 UI，不改写托管页面。 */
;(function () {
  'use strict'
  const stableID = /^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$/

  function pageIdentity(pathname, entry, id) {
    let path = pathname.replace(/^\/p\/[^/]+\/?/, '') || entry || 'index.html'
    try { path = decodeURIComponent(path) } catch (_) { /* 非法转义保留原路径，避免错误合并。 */ }
    const valid = typeof id === 'string' && id.length <= 96 && stableID.test(id)
    return {key: valid ? 'id:' + id : 'path:' + path, path, id: valid ? id : ''}
  }

  function quoteOffset(text, exact, prefix, suffix) {
    if (!exact || exact.includes('\u0000')) return -1
    const matches = []
    for (let offset = text.indexOf(exact); offset >= 0; offset = text.indexOf(exact, offset + 1)) matches.push(offset)
    if (matches.length === 1) return matches[0]
    if (!matches.length || (!prefix && !suffix)) return -1
    const contextual = matches.filter(offset => (!prefix || text.slice(Math.max(0, offset - prefix.length), offset).endsWith(prefix)) &&
      (!suffix || text.slice(offset + exact.length, offset + exact.length + suffix.length).startsWith(suffix)))
    return contextual.length === 1 ? contextual[0] : -1
  }

  function samePage(thread, page) {
    // 稳定 ID 可跨文件名；旧路径锚点不能在另一页面碰巧相同的文字上恢复。
    if (!thread || !thread.page_key || !page) return false
    return thread.page_key === page.key
  }

  function authorLabel(user, application, snapshot) {
    let label = snapshot || (user ? '用户 ' + user : '未知用户')
    if (application && application !== '0') label += ' · 应用 ' + application
    return label
  }

  if (typeof document === 'undefined') {
    if (typeof module === 'object' && module.exports) module.exports = {pageIdentity, quoteOffset, samePage, authorLabel}
    return
  }
  if (window.top !== window.self) return
  const script = document.currentScript
  if (!script || !script.dataset.projectId) return
  const config = {...script.dataset, origin: new URL(script.src, location.href).origin}
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', () => start(config), {once: true})
  else queueMicrotask(() => start(config))

  const styles = `
    :host{font:14px/1.5 system-ui,-apple-system,sans-serif;color:#172c22;color-scheme:light;text-align:left}
    nav,.panel,.footer{font:14px/1.5 system-ui,-apple-system,sans-serif;color:#172c22;text-align:left}
    *{box-sizing:border-box}button,input,textarea,select{font:inherit}button,a,select{touch-action:manipulation}
    button,a{border-radius:7px}button{cursor:pointer;background:#f4f7f3;border:1px solid #ccd8ce;color:#183728;padding:6px 10px}
    button:hover{background:#e5eee2}button:disabled{cursor:wait;opacity:.6}a{color:#235a39;text-decoration:none}
    button:focus-visible,a:focus-visible,textarea:focus-visible,select:focus-visible{outline:2px solid #367b51;outline-offset:2px}
    nav{position:fixed;right:12px;top:12px;max-width:calc(100vw - 24px);display:flex;align-items:center;flex-wrap:wrap;gap:10px;
      padding:9px 12px;background:#fff;border:1px solid #d6dfd4;border-radius:12px;box-shadow:0 3px 16px #0002;pointer-events:auto}
    nav .identity{max-width:160px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:#556553}
    .panel{position:fixed;right:12px;top:76px;width:370px;max-width:calc(100vw - 24px);max-height:calc(100dvh - 96px);overflow:auto;
      padding:16px;background:#fff;border:1px solid #d6dfd4;border-radius:12px;box-shadow:0 5px 24px #0002;pointer-events:auto}
    h2{font-size:17px;margin:0 0 12px}h3{font-size:14px;margin:12px 0 8px}p{margin:6px 0}small,.muted{color:#637365}
    .actions{display:flex;flex-wrap:wrap;gap:7px;margin:8px 0}.targets{display:flex;flex-direction:column;gap:6px}
    textarea{display:block;resize:vertical;min-height:84px;width:100%;border:1px solid #bacbbb;border-radius:6px;padding:8px;margin:8px 0}
    select{max-width:100%;padding:6px;border:1px solid #cad8c9;border-radius:6px;background:#fff}
    .thread,.event{padding:10px 0;border-top:1px solid #e1e8df;overflow-wrap:anywhere}.quote{border-left:3px solid #b7cdb5;padding-left:9px;white-space:pre-wrap}
    .body{white-space:pre-wrap;overflow-wrap:anywhere}.status{font-size:12px;color:#516a56}.notice{padding:8px;background:#fff6dc;white-space:pre-wrap;overflow-wrap:anywhere}
    .notice:empty{display:none}
    .marker{position:fixed;background:#e9bd3440;border-bottom:2px solid #b98218;pointer-events:none}
    .footer{max-width:960px;margin:24px auto;padding:16px;background:#fff9e9;border:1px solid #e2d4ae;border-radius:10px;pointer-events:auto}
    @media(max-width:500px){nav{gap:7px;font-size:12px;padding:7px}.panel{top:94px;max-height:calc(100dvh - 114px)}nav .identity{max-width:90px}}
  `

  function element(tag, attrs, text) {
    const node = document.createElement(tag)
    for (const [key, value] of Object.entries(attrs || {})) node.setAttribute(key, value)
    if (text !== undefined) node.textContent = text
    return node
  }

  function button(text, action, attrs) {
    const node = element('button', {type: 'button', ...attrs}, text)
    node.addEventListener('click', action)
    return node
  }

  function makeHost(footer) {
    const node = element('hs-web-container', {'data-hs-comment-ignore': '', 'data-hs-container': footer ? 'footer' : 'tools'})
    node.style.cssText = footer ? 'all:initial!important;display:block!important;position:relative!important;' :
      'all:initial!important;position:fixed!important;inset:0!important;pointer-events:none!important;z-index:2147483000!important;'
    const shadow = node.attachShadow({mode: 'open'})
    shadow.append(element('style', {}, styles))
    document.body.append(node)
    return {node, shadow}
  }

  function start(settings) {
    if (document.querySelector('[data-hs-container="tools"]')) return
    const api = settings.origin + '/api/web-share/' + encodeURIComponent(settings.projectId)
    const host = makeHost(false), menu = element('nav', {'aria-label': '网页工具'})
    host.shadow.append(menu)
    let identity = null, mode = 'browse', generation = 0, contextSequence = 0
    let panel, notices, list, details, targetsUI, selectionUI, composer, markers, footer
    let observer, routeTimer, paintFrame, cleanup = [], pending = new Set()
    let threads = [], selectedAnchor = null, draftAnchor = null, draft = '', failedWrite = null
    const pageAvailability = new Map()
    let page = currentPage(), route = '', targets = [], root = null, schemaValid = true
    let activeThread = null, detailSequence = 0, listSequence = 0

    function currentPage() {
      return pageIdentity(location.pathname, settings.entryFile, document.documentElement.dataset.hsPageId || '')
    }

    function renderMenu() {
      menu.replaceChildren(element('a', {href: settings.origin + '/'}, '系统主页'), element('a', {href: settings.origin + '/web-share'}, '网页托管'))
      if (identity && identity.project_name) menu.append(element('span', {class: 'identity', title: identity.project_name}, identity.project_name))
      if (identity && identity.user_id) menu.append(element('span', {class: 'identity'}, identity.display_name || '用户 ' + identity.user_id))
      else menu.append(element('a', {href: settings.origin + '/web-share/open?target=' + encodeURIComponent(location.pathname + location.search + location.hash)}, '未登录'))
      menu.append(element('span', {class: 'status'}, mode === 'browse' ? '浏览' : '评论中'))
      if (identity && identity.user_id && identity.can_comment && identity.container_mode !== 'raw') {
        menu.append(button(mode === 'browse' ? '评论' : '返回浏览', () => mode === 'browse' ? enter() : leave()))
        if (mode === 'comment') menu.append(button(panel.hidden ? '展开面板' : '收起面板', () => { panel.hidden = !panel.hidden; renderMenu() }))
      }
      if (mode === 'comment') schedulePaint()
    }

    async function refreshContext() {
      const sequence = ++contextSequence
      try {
        const response = await fetch(api + '/view-context', {credentials: 'same-origin', cache: 'no-store'})
        const result = await response.json()
        if (sequence !== contextSequence) return false
        if (!response.ok || result.code !== 0 || !result.data) throw new Error('身份无法确认')
        const next = result.data
        if (!next.user_id || !next.can_comment || next.container_mode === 'raw' ||
            (identity && (identity.user_id !== next.user_id || identity.release_id !== next.release_id || identity.csrf_token !== next.csrf_token))) leave()
        identity = next
        renderMenu()
        return !!(next.user_id && next.can_comment && next.container_mode !== 'raw')
      } catch (_) {
        if (sequence !== contextSequence) return false
        identity = null
        leave()
        return false
      }
    }

    async function request(path, body, revision) {
      const controller = new AbortController(), ownGeneration = generation
      pending.add(controller)
      try {
        const headers = body ? {'Content-Type': 'application/json', 'X-CSRF-Token': identity.csrf_token || ''} : {}
        if (revision != null) headers['If-Match'] = String(revision)
        const response = await fetch(api + path, {method: body ? 'POST' : 'GET', headers, credentials: 'same-origin', cache: 'no-store',
          signal: controller.signal, ...(body ? {body: JSON.stringify(body)} : {})})
        if (ownGeneration !== generation || mode !== 'comment') throw new DOMException('stale', 'AbortError')
        if (response.status === 401 || response.status === 403) {
          identity = null; ++contextSequence; leave()
          throw new DOMException('identity changed', 'AbortError')
        }
        const result = await response.json()
        if (!response.ok || result.code !== 0) throw Object.assign(new Error(response.status === 409 ? '讨论已更新，请刷新后再操作。' : (result.message || result.msg || '请求失败，请稍后刷新。')), {status: response.status})
        if (ownGeneration !== generation || mode !== 'comment') throw new DOMException('stale', 'AbortError')
        return result.data
      } finally { pending.delete(controller) }
    }

    async function pages(path) {
      let cursor = '', result = [], seen = new Set()
      do {
        const data = await request(path + (path.includes('?') ? '&' : '?') + 'limit=100' + (cursor ? '&cursor=' + encodeURIComponent(cursor) : ''))
        result.push(...(data.items || []))
        if (!data.has_more) return result
        cursor = String(data.next_cursor == null ? '' : data.next_cursor)
        if (!cursor || seen.has(cursor)) throw new Error('分页游标异常，请刷新列表。')
        seen.add(cursor)
      } while (true)
    }

    function showError(error) {
      if (error.name !== 'AbortError' && mode === 'comment') notices.textContent = error.message || '加载失败，请刷新列表。'
    }

    async function enter() {
      const expectedGeneration = generation
      if (!await refreshContext() || generation !== expectedGeneration || mode !== 'browse') return
      mode = 'comment'; generation++; page = currentPage()
      route = location.pathname + location.hash + page.key
      panel = element('aside', {class: 'panel', 'aria-label': '网页评论'})
      panel.append(element('h2', {}, '网页评论'))
      notices = element('div', {class: 'notice', role: 'status', 'aria-live': 'polite'})
      const actions = element('div', {class: 'actions'})
      actions.append(button('刷新列表', loadThreads), button('评论整页', () => compose({kind: 'page', page_id: page.id, label: document.title.slice(0, 120)})))
      selectionUI = element('div'); targetsUI = element('div', {class: 'targets'}); composer = element('div')
      list = element('div'); details = element('div'); markers = element('div', {'aria-hidden': 'true'})
      panel.append(notices, actions, selectionUI, targetsUI, composer, list, details)
      host.shadow.append(markers, panel)
      scanTargets(); renderMenu()
      document.addEventListener('selectionchange', selectionChanged)
      window.addEventListener('scroll', schedulePaint, {passive: true})
      window.addEventListener('resize', schedulePaint, {passive: true})
      cleanup = [() => document.removeEventListener('selectionchange', selectionChanged), () => window.removeEventListener('scroll', schedulePaint), () => window.removeEventListener('resize', schedulePaint)]
      observer = new MutationObserver(records => {
        if (route !== location.pathname + location.hash + currentPage().key) { leave(); return }
        if (records.every(record => {
          const target = record.target.nodeType === 1 ? record.target : record.target.parentElement
          if (target && target.closest('[data-hs-container]')) return true
          const protectedRoot = target && target.closest('[data-hs-comment-interaction="preserve"]')
          if (protectedRoot && (record.type !== 'attributes' || target !== protectedRoot)) return true
          const changed = [...record.addedNodes, ...record.removedNodes]
          return record.type === 'childList' && changed.length && changed.every(node => node.nodeType === 1 && node.hasAttribute('data-hs-container'))
        })) return
        scanTargets(); schedulePaint()
      })
      observer.observe(document.documentElement, {childList: true, subtree: true, characterData: true, attributes: true,
        attributeFilter: ['data-hs-page-id', 'data-hs-comment-schema', 'data-hs-comment-id', 'data-hs-comment-root', 'data-hs-comment-ignore', 'data-hs-comment-kind', 'data-hs-comment-interaction', 'data-hs-comment-label']})
      routeTimer = setInterval(() => { if (route !== location.pathname + location.hash + currentPage().key) leave() }, 250)
      loadThreads()
    }

    function leave() {
      generation++; mode = 'browse'; detailSequence++; listSequence++
      for (const controller of pending) controller.abort()
      pending.clear(); cleanup.forEach(remove => remove()); cleanup = []
      if (observer) observer.disconnect()
      clearInterval(routeTimer); cancelAnimationFrame(paintFrame)
      if (panel) panel.remove()
      if (markers) markers.remove()
      if (footer) footer.node.remove()
      panel = markers = footer = observer = null
      notices = list = details = targetsUI = selectionUI = composer = null
      threads = []; targets = []; selectedAnchor = draftAnchor = activeThread = failedWrite = null; draft = ''; pageAvailability.clear()
      renderMenu()
    }

    // 保护子树只读最外层元数据。普通节点的忽略区、表单、编辑器也不进入文本索引。
    const ignored = '[data-hs-comment-ignore],[data-hs-container],script,style,noscript,template,input,textarea,select,button,form,iframe,canvas,svg,[contenteditable]:not([contenteditable="false"])'
    function scanTargets() {
      const roots = []
      function findRoots(node) {
        if (node.matches(ignored)) return
        if (node.hasAttribute('data-hs-comment-root')) roots.push(node)
        if (node.dataset.hsCommentInteraction === 'preserve') return
        for (const child of node.children) findRoots(child)
      }
      findRoots(document.body)
      const schema = document.documentElement.dataset.hsCommentSchema
      schemaValid = (!schema || schema === '1') && roots.length <= 1
      root = roots[0] || document.body; targets = []
      const counts = new Map()
      function visit(node) {
        if (node.matches(ignored)) return
        const id = node.dataset.hsCommentId, kind = node.dataset.hsCommentKind
        if (id) {
          counts.set(id, (counts.get(id) || 0) + 1)
          const label = Array.from(node.dataset.hsCommentLabel || id).slice(0, 256).join('')
          if (id.length <= 96 && stableID.test(id) && ['text', 'image', 'module'].includes(kind)) targets.push({id, kind, node, label})
        }
        if (node.dataset.hsCommentInteraction === 'preserve') return
        for (const child of node.children) visit(child)
      }
      if (schemaValid) visit(root)
      targets = targets.filter(target => counts.get(target.id) === 1)
      targetsUI.replaceChildren()
      if (!schemaValid) {
        selectedAnchor = null; selectionUI.replaceChildren(); renderReanchor()
        targetsUI.append(element('p', {class: 'muted'}, '页面标记无法校验，可使用整页评论。'))
      }
      for (const target of targets.filter(item => item.kind !== 'text')) {
        targetsUI.append(button('评论' + (target.kind === 'image' ? '图片' : '模块') + '：' + target.label,
          () => compose({kind: target.kind, target_id: target.id, label: target.label, page_id: page.id})))
      }
    }

    function textIndex(scope) {
      let text = '', nodes = [], barriers = []
      function visit(node) {
        if (node.nodeType === 3) { nodes.push({node, start: text.length}); text += node.data; return }
        if (node.nodeType !== 1) return
        if (node.matches(ignored) || node.dataset.hsCommentInteraction === 'preserve') {
          barriers.push(node); text += '\u0000'; return
        }
        for (const child of node.childNodes) visit(child)
      }
      visit(scope)
      return {text, nodes, barriers}
    }

    function selectionChanged() {
      if (mode !== 'comment') return
      selectedAnchor = null; selectionUI.replaceChildren(); renderReanchor()
      if (!schemaValid) return
      const selection = window.getSelection()
      if (!selection || selection.isCollapsed || !selection.rangeCount) return
      const range = selection.getRangeAt(0)
      if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return
      const parent = range.commonAncestorContainer.nodeType === 1 ? range.commonAncestorContainer : range.commonAncestorContainer.parentElement
      if (parent.closest(ignored + ',[data-hs-comment-interaction="preserve"]')) return
      const targetNode = parent.closest('[data-hs-comment-id]')
      const target = targets.find(item => item.node === targetNode)
      if (targetNode && (!target || target.kind !== 'text')) return
      const index = textIndex(target ? target.node : root)
      if (index.barriers.some(node => range.intersectsNode(node))) return
      const exact = range.toString(), startNode = index.nodes.find(item => item.node === range.startContainer)
      let start = startNode ? startNode.start + range.startOffset : -1
      if (index.text.slice(start, start + exact.length) !== exact) start = quoteOffset(index.text, exact)
      if (!exact.trim() || exact.length > 4096 || start < 0) return
      selectedAnchor = {kind: 'text', target_id: target ? target.id : '', exact,
        prefix: index.text.slice(Math.max(0, start - 48), start).split('\u0000').pop(),
        suffix: index.text.slice(start + exact.length, start + exact.length + 48).split('\u0000')[0], page_id: page.id}
      const selected = {...selectedAnchor}
      selectionUI.append(button('评论所选文字', () => compose(selected)))
      renderReanchor()
    }

    function locate(thread) {
      if (!samePage(thread, page) || !schemaValid) return null
      const anchor = thread.anchor || {}
      if (anchor.kind === 'page') return {page: true}
      const target = targets.find(item => item.id === anchor.target_id)
      if (anchor.target_id && (!target || target.kind !== anchor.kind)) return null
      if (anchor.kind === 'image' || anchor.kind === 'module') return target ? {node: target.node} : null
      if (anchor.kind !== 'text') return null
      const index = textIndex(target ? target.node : root), offset = quoteOffset(index.text, anchor.exact, anchor.prefix, anchor.suffix)
      if (offset < 0) return null
      const start = index.nodes.find(item => item.start + item.node.length > offset)
      const endOffset = offset + anchor.exact.length
      const end = index.nodes.find(item => item.start < endOffset && item.start + item.node.length >= endOffset)
      if (!start || !end) return null
      const range = document.createRange()
      range.setStart(start.node, offset - start.start); range.setEnd(end.node, endOffset - end.start)
      return {range}
    }

    function schedulePaint() {
      cancelAnimationFrame(paintFrame)
      paintFrame = requestAnimationFrame(paint)
    }

    function paint() {
      if (mode !== 'comment') return
      const top = menu.getBoundingClientRect().bottom + 8
      panel.style.top = top + 'px'
      panel.style.maxHeight = `calc(100dvh - ${top + 20}px)`
      markers.replaceChildren()
      if (footer) { footer.node.remove(); footer = null }
      const lost = []
      for (const thread of threads.filter(item => item.status === 'open')) {
        const location = locate(thread)
        if (!location) {
          if (samePage(thread, page) || thread.page_path === page.path ||
              (page.path === settings.entryFile && pageAvailability.get(thread.page_path) === false)) lost.push(thread)
          continue
        }
        if (location.page) continue
        const rects = location.range ? location.range.getClientRects() : [location.node.getBoundingClientRect()]
        for (const rect of Array.from(rects).slice(0, 100)) {
          if (!rect.width || !rect.height) continue
          const mark = element('span', {class: 'marker', 'data-hs-container-marker': ''})
          mark.style.cssText = `left:${rect.left}px;top:${rect.top}px;width:${rect.width}px;height:${rect.height}px`
          markers.append(mark)
        }
      }
      if (lost.length) {
        footer = makeHost(true)
        const box = element('section', {class: 'footer', role: 'region', 'aria-label': '无法定位的未解决讨论'})
        box.append(element('h2', {}, '无法定位的未解决讨论'))
        for (const thread of lost) {
          const item = element('div', {class: 'thread'})
          const pathBound = thread.page_key === 'path:' + thread.page_path
          const message = samePage(thread, page) || thread.page_path === page.path ? '原文无法定位' :
            (pathBound ? '页面已删除：' + thread.page_path : '原页面路径不可访问，可能已改名或删除：' + thread.page_path)
          item.append(element('p', {}, message),
            element('p', {class: 'quote'}, (thread.original_anchor || thread.anchor || {}).exact || (thread.anchor || {}).label || '整页讨论'),
            button('查看页尾讨论 ' + thread.id, () => { panel.hidden = false; renderMenu(); openThread(thread) }))
          box.append(item)
        }
        footer.shadow.append(box)
      }
    }

    async function loadThreads() {
      const sequence = ++listSequence
      try {
        const loaded = await pages('/comment-threads?status=all')
        if (mode !== 'comment' || sequence !== listSequence) return
        threads = loaded; pageAvailability.clear(); notices.textContent = ''; renderThreads(); schedulePaint(); checkOtherPages()
      } catch (error) { showError(error) }
    }

    async function checkOtherPages() {
      if (page.path !== settings.entryFile) return
      const prefix = location.pathname.match(/^\/p\/[^/]+\//)
      if (!prefix) return
      const paths = [...new Set(threads.filter(item => item.status === 'open' &&
        !samePage(item, page) && item.page_path !== page.path).map(item => item.page_path))]
      const ownGeneration = generation
      await Promise.all(paths.map(async pagePath => {
        const controller = new AbortController()
        pending.add(controller)
        try {
          const encoded = pagePath.split('/').map(encodeURIComponent).join('/')
          const response = await fetch(settings.origin + prefix[0] + encoded, {method: 'HEAD', credentials: 'same-origin', cache: 'no-store', signal: controller.signal})
          if (ownGeneration !== generation || mode !== 'comment') return
          if (response.status === 404) pageAvailability.set(pagePath, false)
          else if (response.ok) pageAvailability.set(pagePath, true)
        } catch (_) { /* 网络或权限未知时不把页面误报为已删除。 */ }
        finally { pending.delete(controller) }
      }))
      if (ownGeneration === generation && mode === 'comment') schedulePaint()
    }

    function renderThreads() {
      list.replaceChildren(element('h3', {}, '讨论历史'))
      const filter = element('select', {'aria-label': '讨论状态'})
      for (const [value, text] of [['all', '全部'], ['open', '未解决'], ['resolved', '已解决']]) filter.append(element('option', {value}, text))
      const entries = element('div')
      function show() {
        entries.replaceChildren()
        for (const thread of threads.filter(item => filter.value === 'all' || item.status === filter.value)) {
          const item = element('article', {class: 'thread'})
          const anchor = thread.anchor || {}
          item.append(element('strong', {}, '讨论 ' + thread.id), element('p', {class: 'status'}, thread.status === 'resolved' ? '已解决' : '未解决'),
            element('small', {}, authorLabel(thread.author_user_id, thread.author_application_id, thread.author_name_snapshot)),
            element('p', {class: 'quote'}, anchor.exact || anchor.label || '整页讨论'))
          if (!samePage(thread, page)) item.append(element('p', {class: 'muted'}, '其他页面：' + thread.page_path))
          else if (!locate(thread)) item.append(element('p', {class: 'muted'}, '原文无法定位'))
          item.append(button('查看讨论 ' + thread.id, () => openThread(thread)))
          entries.append(item)
        }
        if (!entries.childNodes.length) entries.append(element('p', {class: 'muted'}, '暂无讨论'))
      }
      filter.addEventListener('change', show); list.append(filter, entries); show()
    }

    function compose(anchor) {
      if (mode !== 'comment') return
      draftAnchor = anchor; failedWrite = null
      composer.replaceChildren(element('h3', {}, '新增评论'), element('p', {class: 'quote'}, anchor.exact || anchor.label || '整页'))
      const input = element('textarea', {'aria-label': '评论内容', maxlength: '4000'})
      input.value = draft; input.addEventListener('input', () => { draft = input.value; failedWrite = null })
      const submit = button('提交评论', () => mutate('/comment-threads', {release_id: settings.releaseId, page_key: page.key,
        page_path: page.path, anchor: draftAnchor, body: input.value.trim()}, null, submit, () => { draft = ''; draftAnchor = null; composer.replaceChildren() }))
      composer.append(input, submit, button('取消', () => { draft = ''; draftAnchor = null; composer.replaceChildren() }))
      input.focus()
    }

    function requestID() {
      if (crypto.randomUUID) return crypto.randomUUID()
      const bytes = crypto.getRandomValues(new Uint8Array(16))
      return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('')
    }

    // 写请求不自动重试。失败时保存同一 request_id，只有用户再次明确提交才复用。
    async function mutate(path, body, revision, control, success) {
      if (Object.hasOwn(body, 'body') && !body.body) { notices.textContent = '请填写评论内容。'; return }
      const payload = Object.hasOwn(body, 'release_id') ? body : {...body, release_id: settings.releaseId}
      const ownGeneration = generation, fingerprint = path + JSON.stringify(payload)
      const operation = failedWrite && failedWrite.fingerprint === fingerprint ? failedWrite : {fingerprint, request_id: requestID()}
      failedWrite = operation; control.disabled = true
      try {
        const result = await request(path, {...payload, request_id: operation.request_id}, revision)
        if (ownGeneration !== generation) return
        failedWrite = null; notices.textContent = ''; success(result)
        await loadThreads()
      } catch (error) {
        if (ownGeneration !== generation || error.name === 'AbortError') return
        notices.textContent = error.status ? error.message : '请求结果未知，请先刷新列表核对，避免重复提交。'
      } finally { if (ownGeneration === generation) control.disabled = false }
    }

    function canManage(thread) {
      return identity && (identity.user_id === identity.owner_user_id || identity.user_id === thread.author_user_id)
    }

    function renderReanchor() {
      const old = details.querySelector('[data-reanchor]')
      if (old) old.remove()
      if (!activeThread || activeThread.status === 'deleted' || !selectedAnchor || !canManage(activeThread)) return
      const thread = activeThread, anchor = {...selectedAnchor}
      const control = button('用所选位置重新关联', () => mutate('/comment-threads/' + encodeURIComponent(thread.id) + '/reanchor',
        {anchor, release_id: settings.releaseId, page_key: page.key, page_path: page.path}, thread.revision, control, openThread), {'data-reanchor': ''})
      details.append(control)
    }

    async function openThread(thread) {
      const sequence = ++detailSequence
      activeThread = thread; details.replaceChildren(element('h3', {}, '讨论 ' + thread.id + ' · 事件记录'))
      try {
        const current = await request('/comment-threads/' + encodeURIComponent(thread.id))
        const events = await pages('/comment-threads/' + encodeURIComponent(thread.id) + '/events')
        if (mode !== 'comment' || sequence !== detailSequence) return
        activeThread = current
        details.replaceChildren(element('h3', {}, '讨论 ' + current.id + ' · 事件记录'))
        const kinds = {comment: '评论', reply: '回复', resolve: '已解决', delete: '已删除', reopen: '已重开', reanchor: '重新关联'}
        for (const event of events) {
          const item = element('div', {class: 'event'})
          item.append(element('small', {}, (kinds[event.kind] || event.kind) + ' · ' + authorLabel(event.actor_user_id, event.actor_application_id, event.actor_name_snapshot) + ' · ' + (event.created_at || '')),
            element('p', {class: 'body'}, event.body || ''))
          details.append(item)
        }
        if (current.status !== 'deleted') {
          const input = element('textarea', {'aria-label': '回复内容', maxlength: '4000'})
          const reply = button('提交回复', () => mutate('/comment-threads/' + encodeURIComponent(current.id) + '/replies', {body: input.value.trim()}, null, reply, openThread))
          details.append(input, reply)
        } else {
          details.append(element('p', {class: 'notice'}, '讨论已删除；保留事件记录，重新打开后可继续回复。'))
        }
        if (canManage(current)) {
          const action = current.status === 'open' ? 'resolve' : 'reopen'
          const change = button(action === 'resolve' ? '标记已解决' : '重新打开', () => mutate('/comment-threads/' + encodeURIComponent(current.id) + '/' + action, {}, current.revision, change, openThread))
          details.append(change)
        }
        renderReanchor()
      } catch (error) { if (sequence === detailSequence) showError(error) }
    }

    renderMenu(); refreshContext()
    window.addEventListener('hashchange', leave)
    window.addEventListener('popstate', leave)
    window.addEventListener('focus', refreshContext)
    document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible') refreshContext() })
    window.addEventListener('storage', () => { identity = null; ++contextSequence; leave(); refreshContext() })
    window.addEventListener('account-session-expired', () => { identity = null; ++contextSequence; leave() })
    setInterval(() => { if (document.visibilityState === 'visible') refreshContext() }, 30000)
  }
})()
