# 上游依据与兼容性

本项目面向可嵌入其他 Go 应用的歌词组件库。行为参考 AMLL，原生界面及绘制能力来自 MyGo；不把参考项目放进 WebView 运行。本文区分已经从源码确认的能力、设计选择与仍需实机验证的结论。

## 参考快照

调研日期：2026-11-09。永久链接使用完整提交哈希，上游默认分支后续变化不自动成为本项目的兼容性承诺。

| 上游 | 固定提交 | 用途 | 许可证 |
| --- | --- | --- | --- |
| [MyGo](https://github.com/egoist/mygo) | [`eadf1fce6af3bdb9f7cacf649823de2bee2737f2`](https://github.com/egoist/mygo/tree/eadf1fce6af3bdb9f7cacf649823de2bee2737f2) | Native UI、文字塑形、自绘及 GPU 后端 | [MIT](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/LICENSE) |
| [Apple Music-like Lyrics](https://github.com/amll-dev/applemusic-like-lyrics) | [`f32285864056587c4afa59d310208b0aa829cb53`](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53) | 歌词数据、播放行为、视觉效果和背景的参考 | [AGPL-3.0-only 声明](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/package.json#L4)、[许可证全文](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/LICENSE) |

`ref/` 是本地调研副本，已忽略，不是运行依赖，也不随本库发布。M3 已将固定 AMLL 的词布局、显示规范化、动画和状态算法适配为 Go，来源路径、许可与改动见 [第三方声明](../THIRD_PARTY_NOTICES.md) 及文件头；未复制歌词媒体或背景 shader。M1 已在 `examples/native` 接入上述 mygo 快照，固定模块版本为 `v0.8.4-0.20261109104234-eadf1fce6af3`，提交 `go.sum`，无 `replace`；稳定组件仍待实现，平台验证见 [M1 记录](m1-native.md)。本库默认采用 AGPL-3.0-only。后续移植代码、shader、测试夹具或资源时，必须记录来源、提交和修改情况，保留适用的版权及许可证声明。MyGo 的 MIT 许可不改变 AMLL 原始材料的许可；AGPL 本身也不授予歌词、字体、专辑封面或商标的使用权。

M3 在同一固定快照的公开 API 上增加了内部簇映射、物理右对齐的对唱布局、注释、滚动和受限缓存，未升级依赖或接触 upstream internal。当前实现和验收范围见 [M3 记录](m3-rendering.md)，仍未发布稳定 Native 组件；动态背景继续留待 M4。截图测试的 Go 字体专用许可已另行核对并保留原文。

## AMLL：参考行为与迁移边界

AMLL 不只是歌词 DOM 组件。其[中文说明](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/README-CN.md)列出 core、React/Vue 绑定、完整播放器、歌词格式和 TTML 模块。本库提取库级职责，音频播放、媒体库、网络获取和完整播放器应用交给宿主。

| 参考部分 | 源码依据 | 本库的迁移方向 |
| --- | --- | --- |
| 时间线、重叠演唱、句间高亮、间奏焦点 | [timeline.ts](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/lyric-player/base/timeline.ts) | 纯 Go、可确定性测试的时间查询；playing、highlighted 与滚动焦点分别定义 |
| 布局、手动滚动、seek、弹簧和焦点协调 | [base/index.ts](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/lyric-player/base/index.ts)、[layout.ts](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/lyric-player/base/layout.ts)、[scroll.ts](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/lyric-player/base/scroll.ts) | 状态机和几何数据与 UI 资源分离，滚动后定时恢复跟随 |
| 逐词遮罩、浮动和强调 | [DOM 行实现](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/lyric-player/dom/lyric-line.ts)、[遮罩动画](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/lyric-player/dom/animation/mask) | 以保留的文字布局、裁剪和 Painter 绘制实现；不照搬 DOM、CSS 和 Web Animations 生命周期 |
| 字素、连字、CJK 等文字处理 | [base/line.ts](https://github.com/amll-dev/applemusic-like-lyrics/blob/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/lyric-player/base/line.ts) | 明确文本索引单位；完整行塑形后映射时间片段，保留双向文字和组合字符 |
| 多种动态背景 | [bg-render](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53/packages/core/src/bg-render) | 静态降级先行，随后低分辨率 Canvas + MyGo shader；逐项验证效果与成本 |
| 歌词格式与 TTML | [lyric/src](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53/packages/lyric/src)、[ttml/src](https://github.com/amll-dev/applemusic-like-lyrics/tree/f32285864056587c4afa59d310208b0aa829cb53/packages/ttml/src) | 统一数据模型与格式入口分离；逐格式列明支持范围，不声称已全量兼容 |

AMLL 的 `base` 层仍使用 `HTMLElement`、`EventTarget`、`ResizeObserver` 等浏览器设施，不能将其名称里的 base 理解为可直接替换渲染后端的纯平台无关模块。本库借鉴可观察行为，重新界定 Go 模块边界。上游时间线快照还允许逐帧原地复用；本库必须另行明确自己的借用与复制约定，不能默认为跨线程不可变值。

## MyGo：已经确认的公开 API

以下是该固定提交真实存在的 API，不是拟议的扩展。尚未接入本库的 API 不表示当前已经完成对应功能。

| 能力 | API 与源码依据 | 对设计的影响 |
| --- | --- | --- |
| 原生窗口内容 | `WindowOptions.Content: ui.View(...)`；[Native UI 指南](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/ui/README.md) | Go view 建立容器、交互和可访问性，无 HTML/JS/WebView |
| 自定义绘制 | `Element.Draw`、`DrawOver`；[api_gen.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/api_gen.go#L4860) | 在少量原生元素中绘制可见歌词，避免每个词建立独立控件 |
| 保留的完整文字布局 | `ShapeText`、`ShapeRichTextWith`、`Painter.TextLayout`；[text_layout.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/text_layout.go#L12) | 按文本、宽度、字体、语言和方向缓存塑形及换行结果 |
| 更底层的 glyph | `ui.Shape`、`Painter.Glyphs`；[glyphs.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/glyphs.go#L59) | glyph 按视觉顺序排列，通过 Cluster/Runes 查找逻辑文本范围 |
| 裁剪、变换与离屏绘制 | `Painter.Clip`、`Transform`、`NewCanvas`、`Canvas.Draw/Bitmap`；[绘制指南](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/ui/drawing.md)、[canvas.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/canvas.go#L9) | 逐词高亮可重用完整行布局；背景以受限分辨率离屏渲染 |
| 自定义 shader | `ui/shader.Compile`、`Shader.Draw`；[shader.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/shader/shader.go#L1) | 使用 MyGo 接受的 Kage 风格源代码；WebGL GLSL 不能直接传入 |
| 复用 shader 参数 | `Shader.Uniform`、`NewValues`、`Values.Float/Vec*/Color`；[values.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/shader/values.go#L11) | 初始化时编译并查找变量，热路径重用 Values，避免逐帧构造参数 map |
| 绘制帧与节奏 | `Painter.Now`、`AnimationFrame`、`AnimationFrameRate`、`After`；[paint.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/paint.go#L113) | 仅绘制变化时可不重建 view；静止时停止请求帧 |
| 软件渲染识别 | `Painter.Accelerated`；[paint.go](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/paint.go#L117) | 软件路径关闭昂贵全屏 shader，使用静态背景 |
| 无窗口测试 | `ui.NewTester`、`Render`；[测试指南](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/ui/testing.md) | 可验证布局和 CPU 绘制；不能据此声称 GPU 帧率达标 |

## 线程、索引与资源边界

### UI 主线程与时钟

MyGo 的 view 在 UI 主线程执行，`ui.Element` 只对当前 build pass 有效。不能把 Element 或 Painter 放进持久状态、后台任务和跨帧队列；持久元素身份使用 `ui.Handle`。[Views 指南](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/ui/views.md)明确要求后台结果通过 `Window.Update` 提交；该方法每次都会重建 view，即使闭包没有改变状态，因此应合并高频事件。

`Draw` 只做绘制，MyGo 可能在一帧内调用多次。它不能推进业务状态、消费事件或发送 seek。播放进度从宿主给出的时间锚点、播放速率及 `Painter.Now()` 推导，暂停/seek 时重置锚点；不能累计“假定每次调用等于一帧”的时间差。隐藏、最小化和被遮挡时 MyGo 会停止绘制，恢复时按时间重新求值。自绘动画需要主动尊重 `ReduceMotion`，不能依赖内置控件替它处理。

`AnimationFrameRate(30)` 不是整个窗口的帧率上限：同一窗口的其他元素请求更高频率时，窗口按最高请求绘制。因此背景应自行检查是否需要更新离屏纹理，不能只依赖自己的请求频率。

### 文本索引

`Glyph.Cluster/Runes` 使用 **rune 索引**，`TextLayout` 的 `Caret`、`SelectionRects` 和 `Lines` 等使用 **UTF-16 索引**。Go 字符串切片通常使用 **UTF-8 字节偏移**。三个单位必须显式转换，不能混用。

整行完成塑形与换行，再将逐词时间范围映射到字素/cluster 及视觉片段。不能按一个 rune 等于一个 glyph、一个词等于一个矩形或从左到右递增的 cluster 处理 RTL。`SelectionRects` 和 `Lines` 返回的切片应在布局变更时缓存，避免每帧重新生成。跨 timing 边界的连字、字素簇采用何种高亮策略，需要在原生 spike 中验证并写入测试。

### 资源生命周期的真实能力

- `ui.Shape` 本身不缓存结果，需保留 glyph；完整行布局同样应按明确的缓存键重用。
- `Canvas` 的公开 API 没有 `Resize` 或 `Close`；[canvas.go:96](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/canvas.go#L96) 的 `resize` 是私有方法。本库不能调用它，也不能承诺组件 Close 会同步释放全部 GPU 纹理。采用固定数量、尺寸分桶、解除旧引用的策略；上游资源回收时机必须实测。
- `Bitmap.Update` 适合复用动态图片的身份，避免每帧创建新 Bitmap；[bitmap.go:149](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/bitmap.go#L149)。修改需在 UI 主线程提交。
- `Bitmap.ReleasePixels` 释放的是 CPU 像素，不是 GPU 资源；Canvas 或经过 Update 的图片不能据此回收像素。`DecodeBitmap` 保留原始字节而不复制，字节必须保持只读；[bitmap.go:125](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/bitmap.go#L125)、[bitmap.go:217](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/bitmap.go#L217)。
- CPU shader 是逐像素解释执行，复杂全屏效果可能极慢；[shader 成本说明](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/ui/shaders.md#errors-and-cost)。即使 GPU 编译是异步的，第一次绘制仍可能等待驱动编译，冷启动与稳态要分别测量。

## 工具链与平台条件

接入该 MyGo 快照至少需要 **Go 1.27.3**，以其 [go.mod](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/go.mod#L3) 为准。当前工具链与固定模块版本见仓库根 `go.mod` / `go.sum`。M0 只有纯 Go 核心；M1 为实际原生示例加入固定依赖，不依赖 `@latest` 或开发者机器上的 `replace`。

| 平台 | 原生绘制路径 | 运行条件和降级 |
| --- | --- | --- |
| macOS | Metal | 系统图形和文字设施；无 WebView 前端依赖 |
| Windows | Direct3D 11 | GPU 不可用时可能使用 WARP，需视为软件路径 |
| Linux | GTK 3 的 GtkGLArea + OpenGL | 运行需要 GTK 3；无硬件加速时可能 CPU 绘制；Mesa/驱动内存须计入整进程测量 |

MyGo 的基础原生 UI 是纯 Go、无 cgo 构建；Native UI 应用不需要 Bun 或 npm。WebKitGTK / WebView2 属于使用网页窗口时的条件，不能列作本库原生示例的必需项。宿主自行加入网页、原生扩展或打包功能时，按其所用能力补充依赖。依据：[README](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/README.md)、[安装说明](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/getting-started.md#install)、[渲染说明](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/ui/rendering.md)。

## 验证范围与待验证项

源码确认表明公开 API 可支持原生自绘方案；这与“完整歌词渲染已实现”是两个不同的状态。上游[性能数据](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/docs/ui/performance.md)使用特定机器上的 CPU Tester，不能移作本库的帧率、RSS 或 GPU 内存指标。

本次调研另在临时独立模块中完成了公开 API 冒烟验证：Windows/amd64、Go 1.27.3、`CGO_ENABLED=0`，使用上述固定提交的干净源码作为本地 `replace`，`go test -v ./...` 通过。验证组合包括 `WindowOptions.Content: ui.View(...)` 类型检查、CJK/RTL 混合行的 Shape/ShapeText、TextLayout、glyph 裁剪、Canvas、shader 编译及 Values 参数，以及 `ui.NewTester` 输出像素检查。它是一次无窗口的 CPU 可行性检查，未创建真实原生窗口、未测硬件 GPU、未检验动画性能；临时模块不属于本库交付物，也不替代后续仓内示例与 CI。主仓没有因此引入 MyGo 依赖。

正式原生组件合入前需要补齐以下证据，M1 的已测子集和未测项见 [当前记录](m1-native.md)：

1. 固定依赖版本的可编译原生示例，以及三平台真实窗口运行记录。
2. 长歌词可见行剔除、逐词高亮与完整行换行；CJK、emoji、组合字符、连字、混合 RTL 的几何正确性。
3. 暂停、seek、变速、手动滚动、恢复跟随、多窗口、隐藏恢复、DPI/字体/宽度改变和 reduced motion。
4. 硬件与软件路径的静态降级，首次 shader 编译与稳态分开测量。
5. 暖机后的 CPU 准备时长、分配、实际帧间隔、Go heap、RSS、组件缓存字节和纹理估算；持续切歌/缩放后的上界及解除引用后的回收情况。
6. 组件卸载后的工作取消、回调解绑和引用释放；不把 GC 或驱动回收的时机写成同步保证。

升级上游时提交兼容性 PR：更新固定哈希、受影响 API、许可证差异、运行记录和基准；出现必须访问 `internal/*` 或缺少资源控制 API 的需求时，先讨论上游扩展或降低实现范围，不偷偷 fork 内部后端。
