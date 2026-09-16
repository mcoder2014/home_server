const test = require('node:test')
const assert = require('node:assert/strict')
const fs = require('node:fs')
const path = require('node:path')
const runtime = path.join(__dirname, 'runtime.js')
const api = fs.existsSync(runtime) ? require(runtime) : {}

test('页面以稳定 ID 优先，旧页面回退为解码后的项目相对路径', () => {
  assert.equal(typeof api.pageIdentity, 'function', '缺少页面身份实现')
  assert.deepEqual(api.pageIdentity('/p/trip/', 'index.html', 'trip-guide'), {key: 'id:trip-guide', path: 'index.html', id: 'trip-guide'})
  assert.deepEqual(api.pageIdentity('/p/trip/%E8%B7%AF%E7%BA%BF.html', 'index.html', ''), {key: 'path:路线.html', path: '路线.html', id: ''})
  assert.equal(api.pageIdentity('/p/trip/', 'index.html', 'bad_ID').key, 'path:index.html')
})

test('原文或保存的上下文必须唯一定位，歧义时不猜第一个结果', () => {
  assert.equal(typeof api.quoteOffset, 'function', '缺少保守原文定位实现')
  assert.equal(api.quoteOffset('上午参观博物馆，下午步行。', '参观博物馆'), 2)
  assert.equal(api.quoteOffset('参观博物馆，再参观博物馆', '参观博物馆'), -1)
  assert.equal(api.quoteOffset('上午参观博物馆，下午再次参观博物馆。', '参观博物馆', '下午再次', '。'), 12)
  assert.equal(api.quoteOffset('新内容', '旧内容'), -1)
  assert.equal(api.quoteOffset('新内容', ''), -1)
})

test('稳定页面 ID 跨文件名沿用，其他页面不能误定位到当前页面', () => {
  assert.equal(typeof api.samePage, 'function', '缺少页面隔离实现')
  const page = {key: 'id:trip-guide', path: 'new.html'}
  assert.equal(api.samePage({page_key: 'id:trip-guide', page_path: 'old.html'}, page), true)
  assert.equal(api.samePage({page_key: 'id:other-page', page_path: 'new.html'}, page), false)
  assert.equal(api.samePage({page_key: 'path:old.html', page_path: 'old.html'}, page), false)
})

test('作者同时显示用户与应用标识，避免把应用代写伪装成人工', () => {
  assert.equal(typeof api.authorLabel, 'function', '缺少作者身份展示实现')
  assert.equal(api.authorLabel('123', '456'), '用户 123 · 应用 456')
  assert.equal(api.authorLabel('123', ''), '用户 123')
  assert.equal(api.authorLabel('123', '456', '发布机器人'), '发布机器人 · 应用 456')
})
