# HTML 评论结构标准 v1

适用于为 Home Server 新生成或修改的 HTML/静态构建产物。版本：1。这里定义增强容器用于跨版本定位评论的结构标记；服务端只在响应时注入容器，不改写发布产物。

## 1. 生成原则

保持正常 HTML、原 DOM 层次和业务交互，只在合适的语义节点补充属性。先检查已有页面标记，同一页面更新沿用原标识。上传现成产物不以缺少标记为理由重构或重写页面。

`data-*` 是浏览器支持的自定义数据属性，不是隐藏秘密的渠道。标识只描述页面对象，不放 AK/SK、Token、账号、权限或表单值；身份与授权由服务端判断。[MDN data attributes](https://developer.mozilla.org/en-US/docs/Web/HTML/How_to/Use_data_attributes)

## 2. 属性契约

| 属性 | 放置位置与取值 | 用途 |
|---|---|---|
| `data-hs-comment-schema="1"` | `html`，每文档一次 | 声明本约定版本；未知版本不猜解析规则 |
| `data-hs-page-id` | `html`，每文档一次 | 同项目内唯一的稳定页面 ID，不随文件改名、slug、发布版本或排序改变 |
| `data-hs-comment-root` | `main/article` 等一个正文根，布尔属性 | 限定可定位区域；平台 UI 和业务导航位于根外或标记 ignore |
| `data-hs-comment-id` | 适合讨论的段落组、图片 figure、表格或模块 | 同 page ID 下唯一的稳定对象 ID，不要求每个 span/按钮都加 ID |
| `data-hs-comment-kind` | 与 comment-id 同节点；`text / image / module` | 文字范围、整图、整模块；不要用 ID 类型推断业务权限 |
| `data-hs-comment-label` | 与 comment-id 同节点；可选简短纯文本 | 模块选择列表名称，例如“路线地图”，不得包含用户输入或敏感信息 |
| `data-hs-comment-interaction="preserve"` | 与 kind=module、comment-id 同节点 | 整个子树为原交互保护区；只关联外层模块，不遍历内部选区、代理业务事件或抓取运行时 DOM |
| `data-hs-comment-ignore` | 不参与评论的节点；布尔属性 | 排除自身及整个子树；表单、编辑器输入、敏感区域使用它 |

page-id/comment-id 采用 1～96 位小写语义标识，正则 `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`。例如 `trip-guide`、`route-map`、`day-one-summary`。唯一范围是 `(project_id,page_id,comment_id)`；不能使用数组索引、时间戳、随机 UUID 或完整文本哈希作为每次重生的标识。

嵌套目标选择最接近的显式 comment-id。ignore 的优先级最高，任何父目标的文本/摘要都不得包含其内容。preserve 区域只暴露外层模块和 label，不读取内层动态文本；祖先 text 目标也须跳过该子树。父/子目标均参与时，不能将同一次选择重复绑定两个目标。

页面若声明 Content Security Policy，应允许同源容器脚本、同源 API 请求与容器样式。`script-src 'self'` 可允许同源脚本，但其他更具体指令和 strict-dynamic 仍需核对；平台脚本没有 nonce/integrity，样式为 Shadow DOM 内的 style 元素。不能通过关闭 CSP 规避。无法调整的页面使用原始页面模式；上传前运行 [兼容性检查](html-upload-check.md)。

## 3. 最小结构示例

下面展示静态结构，不加载地图 SDK、不包含凭证，也不提供模拟评论功能。地图应用保留自己已有的 SDK 初始化、容器 ID 和代码。

```html
<!doctype html>
<html lang="zh-CN" data-hs-comment-schema="1" data-hs-page-id="trip-guide">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>旅行方案</title>
</head>
<body>
  <nav data-hs-comment-ignore>原有业务导航</nav>
  <main data-hs-comment-root>
    <section data-hs-comment-id="day-one-summary" data-hs-comment-kind="text">
      <h1>第一天行程</h1>
      <p>上午参观博物馆，下午沿河步行。</p>
    </section>
    <figure data-hs-comment-id="route-map" data-hs-comment-kind="module"
            data-hs-comment-label="路线地图" data-hs-comment-interaction="preserve">
      <figcaption>路线地图</figcaption>
      <div id="map-container" style="height: 360px"></div>
    </figure>
    <form data-hs-comment-ignore>
      <label>行程备注 <input name="notes"></label>
    </form>
  </main>
</body>
</html>
```

`figure` 在页面初次构建时就是业务模块边界；不能为加评论在已初始化的地图容器外临时包裹、移动、替换节点。现有项目已有外层容器时直接加属性即可。静态图片使用相同方式标记 figure，kind 设为 image，并保留图片 alt；可交互图库设为 module + preserve。

## 4. 浏览与评论交互契约

| 场景 | 容器规则 | 原网页 |
|---|---|---|
| 未登录、身份未知或登录失效 | 只允许浏览；隐藏评论模式切换、计数/面板/高亮/选区按钮/页尾评论和写入口 | 保留正常导航、输入、复制、地图及其他交互 |
| 已登录，默认浏览模式 | 菜单显示“浏览”；仅有权用户可主动切到“评论”；不加载评论正文与选区监听 | 原网页交互照常 |
| 已登录，主动评论模式 | 显示评论列表与普通文本选区入口；模块从评论面板的目标列表选择 | 不给地图、图表、播放器、编辑器加截获手势的遮罩 |
| 从评论回到浏览 | 撤掉评论标记和可聚焦入口，销毁容器自己的监听/观察器，取消未提交选区、草稿和请求 | 不重载页面，不重新创建地图，不解绑业务自己的监听 |

浏览模式是当前标签页的界面模式，不改变项目权限或 AK/SK 应用权限。刷新/新标签/换账号后默认浏览；URL 里的 comment 参数不自动开启评论。退出或身份切换清空旧账号的评论数据与草稿，过期异步响应不能重新挂载 UI。

普通文字仅在无 ignore/preserve 的正文区响应原生 Selection；不对整个 document 拦截 click、pointer、touch 或 wheel。可视高亮使用不接收指针的装饰层；`pointer-events:none` 不能代替隐藏、去掉 tab 焦点和卸载监听。平台按钮仅在自己的区域处理事件。[MDN pointer-events](https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/Properties/pointer-events)

## 5. 高德地图与类似模块

地图标记为 `module + preserve`。评论模式从面板目标列表选择“路线地图”，评论关联 map 模块本身；用户仍能在地图上拖拽、滚轮/双指缩放、点击 Marker、打开信息窗或使用工具条。首版不把屏幕坐标当成跨版本锚点，不实现地图内部 POI/地理坐标评论。

容器不能改写地图交互开关、重绑 SDK 事件、调用 map.destroy()/重建地图，或将地图改为静态截图。第三方实例的创建与释放仍由原应用生命周期负责。高德提供自己的交互事件及生命周期能力，评论层应避开这些控制路径。[高德交互与事件](https://lbs.amap.com/api/javascript-api-v2/guide/map/map-bind)、[高德生命周期](https://lbs.amap.com/api/javascript-api-v2/guide/map/lifecycle)

没有标记的旧页面，不猜测 canvas、iframe、自定义组件的内部结构，也不通过像素选取强行挂评论；降级为整页评论或由作者补充稳定外层标记。地图业务 ID 可长期保持，地图中心点、缩放和当前标记数量变化不影响模块身份。

## 6. 更新与校验

| 情况 | 行为 |
|---|---|
| 对象未变，样式、位置、排序、文件名变化 | 保留 page-id 与 comment-id |
| 内容语义被替换成其他对象 | 分配新 ID；不得把旧 ID 复用于无关模块 |
| 文字有 ID 但原引用句子被删除 | ID 只限定搜索范围；仍校验原文，不能凭 ID 将评论贴到新句子 |
| 页面/模块重复 ID | 标记标准校验失败，不随意改 ID 或选择第一个；不影响原 HTML 托管，评论保守降级 |
| SPA | 每个逻辑页面有稳定 page-id；由原页面路由生命周期同步根属性，保证同一时刻一个 root；不篡改原 history/hash |

生成后校验属性版本、唯一性、根数量、类型和嵌套排除规则，检查语义对象是否保留原 ID。浏览器验证应覆盖桌面和窄屏的原业务动作；含实际地图的产物还要验证拖拽、缩放、Marker、信息窗和控件。原始页面模式或未部署增强容器的环境只能报告结构/原网页验证，不能宣称评论兼容已通过。
