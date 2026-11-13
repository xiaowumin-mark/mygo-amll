# M3：AMLL 效果一致性审计

日期：2026-11-09。状态：**未完成，未达到所有者的效果一致要求，不可作为完成的 M3 交付。** 本文纠正基础功能测试代替视觉对照的错误，列出已确认的差距，不把源码可实现性当成运行结果。

最新进展：2026-11-11 已修复滚动/词浮动的整像素阶跃并补齐调度回归，随后所有者反馈当前视觉已非常好。2026-11-12 以该效果为基线做无损 CPU/GPU 优化，实测和验收范围见 [M3 无损绘制优化](m3-performance.md)。本文保留早期失败记录，不将局部对照视为全平台验收。

参考为 AMLL `f32285864056587c4afa59d310208b0aa829cb53`，本地参考仓 HEAD 已核对，源码未修改。核心包为 AGPL-3.0-only；`packages/core/src/utils/spring.ts` 另含 `MIT License github.com/pushkine/` 标记，若移植须另行保留其适用声明。`ref/` 继续只作忽略的调研副本，不成为本库依赖。

## 已确认的实现差距

以下路径相对于该固定快照的 `packages/core/src/`，行号指该快照，不指本仓 Native 原型。

| 项目 | 固定参考规则与源位置 | 返工前基础原型 |
| --- | --- | --- |
| 填色 | 连续渐变 alpha 遮罩，默认过渡宽为词高的 0.5；沿词累计宽度建立轨迹，词间停顿停止推进。`lyric-player/dom/animation/mask/animator-web.ts:123`、`mask/utils.ts:4` | 硬矩形裁剪；逐簇方向填色也与参考固定向右遮罩不同 |
| 词浮动 | `max(1000ms,词时长)`、ease-out 上浮 0.05em，背景翻倍。`dom/animation/float/index.ts:36` | 未实现 |
| 长音强调 | CJK 长词或 2–7 UTF-16 单元的长英文词触发；按字素生成32个采样关键帧，错开缩放、位移、发光和额外浮动。`base/line.ts:80`、`dom/animation/emphasize/index.ts:105` | 未实现 |
| 亮度合成 | bright/dim 双层 plus-lighter 交叉淡入，进入300ms、退出450ms ease-out，暗层最短保留500ms。`dom/line-brightness.ts`、`styles/lyric-player.module.css` | 固定颜色、两次 source-over，不等价 |
| 排版 | 对重组词块平衡换行，含标点、空白及 CJK 代价。`utils/lyric-line-break.ts:12` | 普通自然换行 |
| 焦点锚点 | 行中心对齐 0.35H，顶部为 `0.35H−行高/2`。`base/layout.ts:381`、`:555` | 行顶对齐0.35H；H=600、行高80时已有40 DIP静态差距 |
| 位置弹簧 | 每组独立 posY、背景独立 slide；m=.9。seek/间奏 k=90,c=15；普通播放按100–800ms开始间隔选k=220–170、c=2.2√k；曲终k=140,c=22。`base/index.ts:187`、`base/spring.ts:27` | 一个全局 ω18 临界阻尼弹簧 |
| 行间错开 | 普通 tick 逐行从50ms累计，经过焦点后每步 /1.05；缩放不使用同一 delay。`base/index.ts:967`、`dom/lyric-line.ts:627` | 所有行同步平移 |
| 行缩放 | 非活动播放主行97%、背景75%，活动/暂停100%；主m2/c25/k100，背景m1/c20/k50。`base/group.ts:102` | 未实现 |
| 模糊 | 按焦点距离、视口及交互计算，上限5px；宽≤1024系数.8，播放器 hover取消模糊；400ms CSS ease过渡。`base/index.ts:1065`、`dom/lyric-group.ts:188`、`styles/lyric-player.module.css:304` | 未实现 |
| 背景人声组 | 附属主行、同步时间、按首词先后决定上下位置；折叠不占原有高度，独立±80%位移、.8→1缩放和alpha。`dom/index.ts:180`、`dom/lyric-group.ts:124`、`:202` | 独立常显行，空间和焦点不同 |
| 副行 | 正文→翻译→行音译；字号max(.5em,10px)，行高1.5em，opacity .3。`dom/lyric-line.ts:88`、`styles/lyric-player.module.css:186` | 顺序反了；.6倍字号和固定间距 |
| 对唱 | 右对齐与右侧变换原点；只要整曲含对唱，主侧/对侧分别留15%额外内边距。`styles/lyric-player.module.css:59`、`:319` | 只有右对齐 |
| 逐词音译/Ruby | 每词下方音译，上方居中Ruby，参与词测量和遮罩。`dom/lyric-line.ts:338`、`styles/lyric-player.module.css:76` | 未实现；模型也未承载完整Ruby |
| seek、暂停 | seek不直接定位，取消stagger后使用慢弹簧；暂停时位置继续收敛、背景展开、行缩放回100%。`base/consts.ts:108`、`base/index.ts:1138` | seek直接Snap，缺少相应独立通道 |
| 手势 | 触摸意图阈值>10px、惯性衰减；滚轮150ms去抖后才进入500ms恢复等待。`base/scroll.ts:25` | 4px阈值、无惯性、滚轮立即结束交互 |

这些差距来自未完成的实现，不能用字体或 GPU 差异解释。之前的零分配、解析、seek、窗口 smoke 与 Native 截图自一致性结果仍有各自用途，但不能证明本表任何缺失已完成。

## 已运行的原库参考

本轮在仓库外临时目录构建并运行了固定快照原始 `DomLyricPlayer` 和原始 `parseTTML`，未修改参考源码，没有把原渲染器接入本库或替换 Native 架构。输入为所有者提供的同一份本地 TTML，仅经本机 loopback 服务读取，未复制进仓库。

环境为 Chromium、960×740、devicePixelRatio=1；宿主字体固定 Segoe UI、600、30px，背景RGB(18,21,31)，媒体位置90,000ms，播放器未hover，原库保留播放样式。实际原库输出84行、70个歌词组，支持 mask-image 和 plus-lighter。Native 对应帧媒体位置也核验为90,000ms，字号/字体/背景/尺寸相同，但保留原型自身排版及CPU路径。其行高1.35与参考1.2不同，本身即为待修复差异。

本机临时工件在 `%TEMP%/mygo-amll-reference-20261109/`：`amll-90000.png`、`native-90000.png`。参考画面已可见距离模糊、背景分组与高亮差异。它们用于确认缺口，**不是相似度达标证明，也不是动画逐帧像素金图**。本轮仍未建立可复现的跨后端 CI 对照或对齐的动画时间控制，不能再次用一张静态图宣布100%。

## Native 能力与严格一致的边界

固定 mygo 公开 API 确实支持矩阵变换、shader 文字渐变、元素 Gaussian Blur 和整组 Opacity，因此缺失效果不能归咎为 Native 全部不可实现。以下差异需要明确设计和实际验证：

- `shader.Fill` 不给彩色emoji套同样shader；整词遮罩需要合成纹理后再乘alpha。
- `TextLayout` 未公开 wrapped glyph 的逐簇变换；裁剪矩形切图浮动不能直接当作复杂文字的等价字素动画。
- Painter 没有 Blur/整组Opacity作用域；应选择元素合成或有界Canvas多pass，不能分别改绘制颜色alpha冒充组透明度。
- Native 没有公开 plus-lighter 模式；shader加法只有在输入纹理、隔离/backdrop边界相同时才等价，需与浏览器实际分组比较。
- Canvas.Draw 使用物理像素、子Painter.Scale=1，Canvas也没有公开Close/Resize；DPI、字体光栅、采样、模糊离散核和释放预算需重新验证。

公开依据：[shader.Fill](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/shader/shader.go#L254)、[Blur](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/element.go#L1505)、[Effect/DPI](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/effect.go#L8)、[Canvas](https://github.com/egoist/mygo/blob/eadf1fce6af3bdb9f7cacf649823de2bee2737f2/ui/canvas.go#L9)。这不等于已运行Native效果或证明无法复刻全部行为，也不能保证跨渲染引擎逐像素一致。

完整效果与参数复刻、固定场景轨迹对照，以及同环境逐帧像素一致，是不同的验收断言。所有者已明确选择**保留 Native，完整复刻 AMLL 全部效果、参数和时序，逐项对照验收**。继续原生返工，不接入 DOM/CSS/WebGL 运行时。上表记录的是返工前的明确差距；后续实现须逐项附对照证据，不能只更新状态为已实现。保留所有本地改动，验收前禁止提交远程。

## 2026-11-10 返工验证

当前参考路径已接入独立组弹簧、背景折叠、逐词渐变、词浮动/字素强调、亮暗双层、组 Gaussian/透明度和根加法混合。普通示例启用该路径，旧基础路径只保留作诊断/历史测试。尚未完成以下整体验收，不将“接口已接入”写成100%完成。

同一授权 TTML 已在实际 Windows D3D 窗口中绘制，且与未修改原库分别在90,000ms和140,000ms作了持有媒体位置的对照：Segoe UI、600、30px、960×740**客户区**、DPR1、hover=false、RGB(18,21,31)。必须补偿系统窗口边框取得实际客户区；`WindowOptions.Width/Height`不能直接当作截图的内容像素尺寸。140s场景含展开的背景歌词。两侧84行/70组，90s焦点组31、高度82.5 DIP，距离模糊数值一致。Native90s效果缓存估算28,134,304字节，140s为30,751,400字节，未触发降级。

真实窗口截图使用客户区屏幕 BitBlt 读取已呈现像素，没有把公开 `Window.CapturePage`当作 GPU 截图：该固定 mygo 版本的 Content capture 会对最后场景执行 CPU raster。独立480×260 D3D小窗口还覆盖emoji、词强调/发光、父组模糊及根加法：30次draw均`Accelerated=true`、无shader错误。首帧含编译/准备约1073.5ms，不能据此宣称冷启动或稳态帧时间达标。

对照暴露了仍需解决的问题：90s正文Native可见墨迹比原库低约1px，译文也约低1px、笔画覆盖较少；组位置只差约0.1 DIP，峰值亮度接近，不能通过随意提高opacity掩盖字体/光栅/基线差异。固定截图仍未控制完整动画壁钟轨迹，不是逐帧金图。DPR2在90s时估算62,204,816字节且无降级，但9s、65s、140s背景场景触发64 MiB效果预算，**不能视为这些常用HiDPI场景已经保真通过**。仍须优化资源并重新验证，不提高预算或用静态回退代替验收。

参考宿主已整理为可重跑的 `scripts/amll_reference.py` 与同目录TS宿主：仅在临时目录构建固定快照、仅loopback提供四个精确路由，Node依赖由外部工具目录提供，输入不复制进仓库。Native诊断入口为`TestReferenceSceneCapture`，可输出布局和完整CPU shader图；CPU全画布解释执行极慢，初次全图捕获两分钟尚未结束后已中止，不能列作已通过。复跑说明见[对照验证](m3-parity-validation.md)。全部生成图片、日志和歌词继续留在本地临时目录。

### 后续定位与定向验证

以下是返工期间的局部证据，仍不等于全部视觉效果、平台和性能验收完成。

| 范围 | 已取得证据 | 尚不能推出的结论 |
| --- | --- | --- |
| 宿主更新顺序 | 执行未修改原 Base/Dom 数值方法，并以真实浏览器 A/B/C 轨迹交叉检查；普通播放、前后 seek、组件 resize 与滚轮场景最大数值误差约 2.98e-13。已修复先 advance 后设目标导致的一帧滞后，以及 dots 的 delta 偏移 | 不覆盖全部冷启动与 DOM 字体/像素 |
| 同时间重复绘制/resize | 有界帧基线回滚，500→560→500、重复 box、相同时间 clock/seek、offset clamp 与过期 worker 结果回归通过；净时间只推进一次 | 不替代真实快速 resize、隐藏恢复与跨屏 DPI 验证 |
| Windows CSS 基线 | 原 Chrome 157，四字体×十字号×三行高，共 120 向量，在 DPR1/2 得到相同基线；使用 primary-font rounded ascent/descent 加向下取整 half-leading。Ruby、正文和词音译各行保留独立 drawY | 不证明其他平台基线或 Native 字形覆盖像素与 Chrome 相同 |
| CSS 内容宽度 | W960/font30/含对唱时，原 wrapper content900，line padding6/135，正文 content759；已修正先前765的错误。背景按自己21px字号扣 padding | 不证明所有自定义字号/极窄宽度的换行都一致 |
| 展示选项 | 32 种文本优化组合×31 合成输入，共992组与原 Node 输出对照；首 UTF-16 单元遮蔽、孤立代理的显示已用真实 Chrome 分段和像素检查 | 不等于所有 TTML 私有扩展或 Unicode 字体组合兼容 |
| Windows bidi | 30 个非 emoji 向量与实际 LTR DOM 严格对照，包含嵌入、隔离、控制符和混合方向 | 不代表 emoji 的 OpenType script、字宽和颜色正确 |
| HiDPI 效果预算 | 排除零透明度资源、dim-only 模式、两张 Gaussian surface 和 atlas 重排后，授权输入字号37/DPR2的1800帧及12次hover未降级，估算峰值约66.83MB，仍低于64MiB | 不代表整进程 RSS、GPU 完成时间或所有长曲/字体都达标 |
| 输入和静止调度 | 静止鼠标随行变换重新命中；paint/hit 使用本帧背景 slide；暂停收敛、曲终收敛和隐藏后 wake=0 的定向回归通过 | 不替代设备真实触摸、读屏器和整机后台功耗测量 |

emoji 的三项根因已有正式候选路径：公开 `Glyphs` 的 CPU 与真实 D3D 都缺少系统 COLRv1 渐变，现改用明确初始化的字体 owner、公开 HarfBuzz shaping 与 Native Path 冷栅格；以完整 grapheme 选择字体，按实际脚本和方向 shape，消除 synthetic-bold advance 及 Hebr GPOS 差异。Windows 系统 Segoe UI/Emoji 范围内，新增36个真实DOM字宽向量通过，原33个bidi向量中的三条emoji已取消opt-in跳过；emoji/bidi等专项race通过。未知、已注册、italic、variable字体和缺失能力均明确报告，不暗中退回扁平颜色。selection cache最多64项、保守≤16KiB；不缓存全文glyph或隐式读取字体。

实际系统字体3487个color base glyph包含10种paint，编译遍历均通过；这些不是3487张像素金图。正式provider的四个sprite与实际DOM对照仍有轮廓/颜色采样误差，尚在分离定位，不能据字宽正确宣称像素一致。新的真实D3D完整歌词窗口已显示彩色emoji（960×740、Segoe UI60030、335次draw、Accelerated=true、效果估算14,148,552B、无降级），这也是局部可运行证据。

此前另一项明确差异是 Windows 中文回退：Chrome 实际使用 Noto Sans SC/Bold，而旧 Native 路径选择 Microsoft YaHei regular。现已按 host language、系统 locale、实际 BMP glyph 覆盖和 Chromium 脚本优先级补足公开 family/weight 选择，23 个真实源上下文及公开字体轮廓检查通过。该纠正限于已验证的正常系统 Latin 主字体，不是全浏览器字体匹配；不能把未支持字体的笔画差异归为可忽略容差。

### Windows 字体、物理行与尺寸的新增对照

以下证据来自 Chrome 157、本机固定系统字体与固定 AMLL 快照。源码 oracle、真实 DOM 尺寸和 Native 像素是不同证据，分别列出。

| 项目 | 当前对照结果 | 范围和限制 |
| --- | --- | --- |
| 宿主时序 | 7 个原 Base/Dom 方法场景最大数值差约 `3.84e-13`；包含冷启动、seek、隐藏恢复、跳过区间和曲终历史 | stub DOM 执行原数值方法，另有真实浏览器轨迹；不是全部异步 DOM 或 GPU 轨迹 |
| 翻译/行音译自然换行 | 原 DOM 1,216 个普通输入及 1,050 个 SHY/LS/PS 输入对照通过；198 条固定向量 | 保留整段 script 和 ICU line bidi；受每行 16 KiB、1,024 物理行及有界测量工作量限制 |
| 静态正文 | 原 DomLyricPlayer 全曲每行至多一词的 24 例通过，覆盖 Hebrew emoji 上下文、SHY、空硬行和左右溢出 | 保留原平衡算法 BR 分区，再在各硬段内自然换行；硬 BR 不把尾 SHY 画成连字符 |
| 系统 DPI | CSS block、inline 和 Ruby 的系统 1×/1.25×/2×尺寸矩阵通过；另有 184 条系统 DPI 临界换行向量 | Chrome 启动 device scale 与 CDP 仅提高栅格 DPR 不同；尚无 Native 真实跨屏 DPI 轨迹 |
| 文本 emoji | 240 条源向量覆盖 Shape/Resolved/Range 等入口；182 个 BMP scalar × plain/VS15 × 12 个字体/字重组合，共 4,368 条即时 DOM/CDP 字宽通过 | 正常 Segoe UI、Arial、Times New Roman、Calibri、Consolas 及空默认，400/600；不把 italic 仅测宽事实算作 Native 通过 |
| 字体替换 | 144 条原平台事实保留 normal/italic 的实际选择；Native 彩色路径只放行已验证 normal 系统 family 与公开 glyph proof | unknown、registered overlay、italic、variable、补充平面文本回退及未验证特性仍有 guard；普通 Latin 可继续走原 Native 可读路径 |
| 间奏尺寸 | 39 组原 CSS/ResizeObserver 向量通过；真实 paint bbox 与 JS Measured 尺寸分开 | 例如 font30.3/system2：实际 `98.7578125×39.3828125`，RO `98.75×39.375`；只改盒测量，不量化 motion 时间和 JS margin |

自然换行使用原 Chromium 的物理布局单位规则：`ceil(shapeWidth×scale×64) <= floor(available×scale×64)+1`，不是扩大视觉误差容限。物理行保留原 paragraph 的 script、bidi 与 rune 区间；硬边界末尾的 SHY 仅从 paint 范围去除，空行高度仍保留。字号、系统比例与 HTML 空白归一共同进入冷几何准备，Draw 不重新分词或读取字体。

Windows mono resolver 保留四个 CJK 加一个 Symbol 的 BMP bitset，总计不超过 40 KiB；各 weight 的稀疏 primary 覆盖最多 `9×512B`，共享 ICU key 不超过 1 KiB，含 alias header 的其余 metadata 保守小于 16 KiB。初始化使用至多 64 KiB 临时 glyph-set 缓冲，不保留 GDI handle；冷 Shape/Resolved/Range 无 GDI 调用或 I/O。该预算独立于颜色字体 owner 与效果纹理预算。

当前其他平台没有 Windows ICU/GDI 纠正路径，只保留公开 UI 的可读降级，明确为 inexact。Linux/macOS 交叉编译不能代替真实窗口、字体或效果验收。本机 color Path 与 Chrome 的边缘覆盖仍有已知差异；字宽、画笔类型遍历和参数测试通过均不能消除这一事实。

### 静态内容和最终性能复核

静态、软件及 reduced-motion 分支现使用同一受预算约束的静态 atlas，保留逐词 Ruby、音译、字素盒和独立副行；不再用整行正文代替这些内容。预算或字体能力拒绝时，保底 retained-layout 仍绘制全部文字，并保留明确的未验证/资源错误。两个分支的双行 Ruby/音译冻结 CSS 盒与独立公开 Glyphs 像素对照通过，误差至多 1 个通道值；这不是全浏览器像素金图。静态播放用既有有序词边界定时刷新阶梯高亮；暂停或曲终不持续申请帧。

本轮 warm 准备基准使用正式字体 provider、960×740 逻辑视口，200/2,000/10,000 行、每项五次、`-benchtime=1s`。各项均为 `0 B/op, 0 allocs/op`，中位数分别约 39.9/49.1/59.4 µs，范围 37.9–61.2/42.9–56.0/55.4–101.2 µs；效果估算约 16.4/17.1/17.9 MB。该基准持有媒体位置并在计时外重置 checkpoint 历史，不包含真实 view 构建、GPU 绘制或冷加载。

真实 D3D 新增同输入合成场景，覆盖逐词 Ruby/音译、行翻译、背景、对唱、Hebrew family emoji 及 Arabic。原播放器和 Native 均保持正常播放样式，持有媒体 2s，等词动画稳定后捕获；Native 960×740 客户区、DPR1、Accelerated=true、效果约 9.43 MB、无效果降级。它补充了真实可运行证据，尚不构成全部时钟事件和像素一致证明。

**最终窗口性能仍未通过。** 在其他代理未运行重测试的时段，12 秒实际窗口测得：授权输入90s起继续播放的 view 间隔 p50=35.589ms/p95=78.479ms，效果约19.35 MB，OS peak RSS约358 MB；稳定140s的另一轮 p50=35.598ms/p95=77.175ms，效果约16.43 MB，OS peak RSS约334 MB。后者客户区截图两次被可见性/遮挡检查拒绝，不能把对应屏幕图列为有效捕获。合成 rich 场景 p50=34.849ms/p95=77.737ms，OS peak RSS约466 MB。这些是短窗口进程观测，不是长期资源上界；保留失败结果，不用早先16.71ms的局部记录代替。

公开 API 的独立最小窗口又顺序测了空白加2px标记、retained TextLayout 和相同根 LayerEffect：本机显示报告60Hz、DWM composition=true，Painter均Accelerated=true，三个阶段的view间隔p50约34.3/34.3/78.0ms。普通空白窗同样存在节奏基线，根合成还带额外成本；当前不能把全部差距归到歌词算法或单一等待分支。view时间戳、运行时“present”提交耗时与实际GPU完成/屏幕呈现仍是不同指标。未取得逐呈现事件、跨屏 DPI、长时切换回收和其他平台原生证据，M3继续保持未完成。

2026-11-10 18:51，中断后使用**同一可执行文件、没有性能代码改动**重测稳定140s，客户区可见性检查和屏幕捕获成功。12秒窗口为668次draw，预热后595个view间隔：p50=16.660ms、p95=17.872ms、p99=19.202ms；效果估算16,432,400B，OS peak RSS346,996,736B，无效果降级。它说明此前慢记录不能直接归因为固定的歌词算法成本，但不能消除跨轮波动或作为已修复证明。

固定mygo源码中`Context.AnimationFrame`优先于`Painter.After`，`RequestFrame`并无固定16ms定时器；没有找到无条件的“双重vsync”链。D3D轻帧分支会在`Present(0)`后异步等DWM，60Hz的等待上限为两个刷新周期，与34ms观测吻合，却尚未证明当时确实走该分支。先前最小窗口`WindowActive=true`也不能证明实际前台/未遮挡：mygo在从未获焦点时同样可能绘制active，Windows Occluded只检查最小化。后续定位应持续验证窗口可见性，并保持提交、view和实际呈现的指标分离。

随后公共API v2探针使用AlwaysOnTop/固定位置，逐view核对客户区中心点和前台PID，四阶段均为960×740/DPI96、Accelerated=true、visible=true且未最小化。仅Context.AF、AF加Painter.After、根效果AF、根效果AF加After四阶段，各约2.5秒，p50/p95分别为16.620/17.538、16.654/17.458、16.677/17.648、16.622/17.558ms。只有相邻两次采样均越过500ms预热窗口的间隔才计数；根效果AF有一次34.199ms间隔。没有复现固定的双请求30fps或根效果78ms，也没有修改产品或依赖作为“修复”。该短窗口只验证中心点，不证明完整客户区和长期呈现稳定性。

同日重新构建并启动干净原库，以140s初始媒体、960×740/DPR1、Segoe UI60030px、hover=false捕获新稳态图，与最新Native实际屏幕图比较。主行及背景组整体盒位置接近，但背景正文与译文仍有约1–2px的可见栅格位置/覆盖差异；数值盒与baseline目标相同还不足以排除局部atlas栅格化、采样相位或字体光栅差异。本轮没有添加固定像素偏移掩盖它，也不把最新图标为像素等价。

本轮最终`python scripts/check.py`完整通过，Native race测试134.016s，Python检查器3项通过；另有扩展原库/Chrome/正式字体与授权输入命令通过（Native29.309s、motion3.078s），新增fallback/静态word wake定向测试通过。`CGO_ENABLED=0`的linux/amd64和darwin/arm64构建通过。全部结果为基于`24d0740`的本地未提交工作区，没有commit、push、PR或发布；普通检查通过仍不改变M3未完成状态。

## 2026-11-11 亚像素动画与调度修复

根因已经通过 CPU 与真实 D3D 前后对照确认。固定 mygo 的 `ShaderEffect` 在提交目的矩形时先做物理像素取整；旧代码直接改变矩形 X/Y，因此滚动及普通词浮动的亚像素位移会丢失。`imageSample` 已是双线性过滤，问题发生在目的原点。现在通过公开 `Painter.Transform(Translation)` 绘制局部原点的 shader，保留词、行、组与静态 atlas 的小数位置；原参考弹簧、时间、舍入规则和依赖版本不变。

同时删除 `drawReference` 的 paint-only `After`。回归实证旧实现会在没有新 view 时间戳的 100ms 内重复绘制六次旧姿态；新实现只消费已准备帧。`referenceViewFrame` 在动态 idle 输入或客户区尺寸变化后申请一次完整确认帧，覆盖 paint 才发现的布局/DPI 变化；链收敛后停止，不保存 `Context` 或在 Draw 中推进时钟。

新增测试覆盖 1×/1.25×/2× 的 X/Y 物理像素连续位移，60/120 Hz 主词和背景词上浮，父变换组合，暂停 resize、同尺寸布局变化及分数 DPI 后的继续动画与停帧。用临时 Go overlay 恢复旧直接 Draw 后，12 组真实词浮动轨迹均在第一帧失败；修复后通过，轨迹质心容差 0.015 物理像素。父变换测试的硬矩形边缘覆盖容差为 0.125 像素，不把它用作文字金图容差。

真实窗口使用同一合成文字组和单像素脉冲，960×740 客户区、DPI96、Scale1、Windows D3D；九张 BitBlt 屏幕捕获逐张核验进程 PID 和 3×3 客户区网格可见性。每步平移 0.125 物理像素：

| 位移 px | 修复前文字 Y 质心 px | 修复后文字 Y 质心 px | 修复后脉冲 Y 质心 px |
| ---: | ---: | ---: | ---: |
| 0 | 63.991177 | 63.991177 | 162.500000 |
| 0.125 | 63.991177 | 64.094517 | 162.625490 |
| 0.250 | 63.991177 | 64.208144 | 162.750980 |
| 0.375 | 63.991177 | 64.337476 | 162.876471 |
| 0.500 | 64.991177 | 64.456318 | 163.000000 |
| 0.625 | 64.991177 | 64.606486 | 163.123529 |
| 0.750 | 64.991177 | 64.733539 | 163.249020 |
| 0.875 | 64.991177 | 64.875022 | 163.374510 |
| 1 | 64.991177 | 64.991177 | 163.500000 |

这证明屏幕上的整像素阶跃得到修复。两轮全部 draw 均 Accelerated=true、无效果降级，效果估算峰值同为 12,767,696 B，没有增加持有的纹理预算。

随后各六秒正常合成歌词播放，排除前 500ms，只有相邻采样都通过可见性/前台 PID 检查才计数：

| view 间隔 ms | 修复前（322 个） | 修复后（320 个） |
| --- | ---: | ---: |
| p50 | 16.659 | 16.692 |
| p95 | 18.567 | 24.347 |
| p99 | 33.749 | 33.268 |
| max | 41.020 | 50.720 |

该短测没有证明帧率或尾部延迟改善，不能将亚像素修复称为全面性能达标；view 间隔也不是 GPU 完成或逐呈现事件。原始源码快照、前后程序、探针、屏幕图与 JSON 仅在本机 `%TEMP%/mygo-amll-m3-smooth-20261111`，不分发或加入版本控制。完整同事件动画矩阵、跨屏 DPI、长期切换回收、macOS/Linux 实窗及所有者签收仍需独立记录。

本轮暖态准备的前后探索基准为 `BenchmarkReferenceWarmPrepare`，960×740、正式字体 provider、相同合成输入，各三次；Windows amd64、Go 1.27.3、i5-1135G7。每格按运行顺序列出 ns/op：

| 行数 | 修复前 | 修复后 | 效果估算 B（前后相同） |
| --- | --- | --- | ---: |
| 200 | 37816 / 33236 / 37272 | 36524 / 33929 / 32676 | 16,411,624 |
| 2,000 | 40927 / 33937 / 34331 | 33873 / 34379 / 34519 | 17,143,848 |
| 10,000 | 39355 / 33225 / 33142 | 33637 / 33633 / 33363 | 17,912,936 |

以上均 `0 B/op, 0 allocs/op`，不包含 view 构建、绘制提交、GPU 或 checkpoint 吞吐；三次探索不替代五次稳定性能门槛，不据此声称渲染加速。

最终检查：`python scripts/check.py` 完整通过，含 Native race 268.512s、全包 vet/race、三项 Python 检查和空白检查（本轮检查使用 Python 3.14.6、GCC 16.1.0）。固定 TS spring/词效果、宿主调用顺序、Chrome WAAPI/CSS/字体与 emoji 对照完成；三个即时 CDP 测试首次因 Python 不在 PATH 失败，补入既有 Python 3.13.16 后定向重跑全部通过，包含 240/4,368 个文字 emoji 向量与 144 个字体替换向量。未提供本轮授权 TTML、独立颜色 DOM PNG、静态正文 DOM JSON 及 opt-in CPU 全图路径，相应外部诊断跳过，没有把它们计为通过。

正式 Windows 示例 `-smoke-duration 6s` 退出 0，Accelerated=true、Scale1，完成暂停/seek、resize、隐藏恢复、2×及关闭。Linux/amd64、macOS/arm64 的 CGO_ENABLED=0 构建通过，不能代替实窗。另复用合成 rich 场景与原库稳态图，held2s、Segoe UI600/30px、960×740/DPR1，真实 D3D 屏幕图无降级、效果估算 9,431,904 B。主行正文、翻译、Ruby、音译的亮度质心相对原库仍约 +0.113/+0.319/+0.300/+0.311px，背景正文/翻译约 +0.123/+0.008px；仍有栅格相位与覆盖差异，不是像素一致。旧 140s 授权输入未在本轮重验，原 1–2px 差异记录继续保留。上述开发候选等待所有者使用自己的实际输入验证签收。
