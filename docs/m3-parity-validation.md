# M3 原库对照验证

状态：2026-11-10，返工验证中，未通过完整效果验收。普通Go检查和CPU小图不能代替此验证，也不授权远程提交。

## 原库宿主

先准备忽略的原库快照`ref/applemusic-like-lyrics`，HEAD必须为`f32285864056587c4afa59d310208b0aa829cb53`且无跟踪文件修改。可选Node工具目录放在仓库外，需要`esbuild`、`@ungap/structured-clone`、`bezier-easing`、`deep-freeze`、`gl-matrix`；它们不是本库依赖。已有工具目录时无需再次安装。

```powershell
python scripts/amll_reference.py --ttml '<本地授权TTML>' --node-modules '<外部工具目录>/node_modules'
```

打开`http://127.0.0.1:43821/`，浏览器视口设960×740、DPR1。默认Segoe UI600、30px、媒体90,000ms。`window.reference.stats()`报告实际参数；`layout()`输出已挂载组的盒、transform、opacity、filter和字体。`setTime(ms)`作显式seek并持有媒体位置，`play()/pause()/resume()/hold()`分别控制宿主和原库传输。媒体持有不暂停壁钟词动画，因此这仍是**真实浏览器时钟宿主，不是确定性的全动画回放**。

截图前将指针移到页面右下角1×1像素的“Capture neutral area”按钮，确认stats的`hover=false`；指针停在歌词上会取消原库距离模糊并出现hover背景。按Ctrl+C停止loopback服务。脚本不监听公网、不复制输入、不自动安装包、不修改参考源码。

## Native 布局/CPU shader诊断

```powershell
$env:MYGO_AMLL_PARITY_CAPTURE = Join-Path $env:TEMP 'mygo-amll-parity'
$env:MYGO_AMLL_TEST_TTML = '<本地授权TTML>'
$env:MYGO_AMLL_PARITY_AT = '90s'
$env:MYGO_AMLL_PARITY_SCALE = '1'
$env:MYGO_AMLL_PARITY_FONT_SIZE = '30'
$env:MYGO_AMLL_PARITY_LAYOUT_ONLY = '1'
go test ./examples/native -run '^TestReferenceSceneCapture$' -v -count=1
```

省略输入环境变量使用合成歌词。诊断固定960×740/Segoe UI600，默认30px，字号可由上述环境变量调整；以60Hz显式更新3秒并持有媒体位置，输出`native-layout.json`，效果降级会使测试失败。删除`MYGO_AMLL_PARITY_LAYOUT_ONLY`可额外执行完整CPU shader截图，解释执行很慢；这不是实际GPU帧，也不能直接与不同传输历史的浏览器帧作像素等价断言。生成输出禁止加入版本控制。

真实窗口运行`go run ./examples/native -ttml '<本地授权TTML>' -font 'Segoe UI' -font-size 30 -at 90s`。示例有控制栏，歌词客户区大小和媒体位置必须单独记录；整窗口尺寸不能直接与原库的歌词视口比较。真实D3D帧应读取实际呈现的客户区像素，并记录Accelerated、物理尺寸、DPI、hover及所有降级。公开`Window.CapturePage`在固定mygo版本中使用CPU场景重放，不能作为GPU framebuffer证据。

## 逐项证据范围

原TS数值oracle测试分别位于`internal/motion/reference_node_test.go`、`reference_interlude_test.go`及Native的presentation/geometry/effects参考测试中，按各测试的环境变量启用。参考仓或外部Node缺失时跳过，不让运行库依赖参考引擎。它们可验证算法分支与浏览器关键帧插值，不能证明字体、完整排版、资源生命周期和真实GPU呈现全部一致。

Windows 源码、浏览器和已安装字体的扩展对照可按下面的环境变量运行。`bezier.mjs` 是仓库外固定 `bezier-easing@3.0.1` 的模块，Chrome 路径由本机安装决定；不需要改 `go.mod` 或安装本库运行依赖。

```powershell
$env:AMLL_REF_ROOT = (Resolve-Path 'ref/applemusic-like-lyrics').Path
$env:AMLL_BEZIER_SOURCE = '<仓库外固定模块>/bezier.mjs'
$env:AMLL_CHROME = '<本机 Chrome>/chrome.exe'
$env:AMLL_CHROME_BINARY = $env:AMLL_CHROME
$env:MYGO_AMLL_TEST_EMOJI_FONT = Join-Path $env:WINDIR 'Fonts/seguiemj.ttf'
$env:MYGO_AMLL_TEST_TTML = '<本地授权 TTML>'
go test ./examples/native ./internal/motion -run 'TestReference|TestStaticBody|TestEffectsExternal|TestEmojiColor' -count=1
```

`TestReferenceTextEmojiBMPAgainstChromium` 会即时采集原 DOM/CDP 的 4,368 条字宽。字体指纹不符时相应记录测试会跳过，必须报告跳过原因，不能算作本机已通过。静态正文的 24 例完整原播放器诊断另接受 `MYGO_AMLL_STATIC_BODY_DOM` 指向仓库外捕获 JSON；没有该文件时，仓内固定 SHY、溢出与脚本上下文回归仍会运行。普通 CI 不强制安装 Chrome 或加载大型系统颜色字体。

真实截图还必须对齐动画壁钟：只固定媒体位置不会冻结原 Web Animations，冷启动后等待三秒与七秒的图可能处于不同词浮动阶段。稳定帧比较应记录等待时间与实际动画状态；动态比较应使用相同事件序列和采样时刻。窗口 view 间隔是宿主更新间隔，不是 GPU 完成或实际呈现间隔。

未完成项继续以[一致性审计](m3-amll-parity.md)为准，记录失败与未测场景；不要根据单张静态图或本实现自一致性更新为“100%”。
