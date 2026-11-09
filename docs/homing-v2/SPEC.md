# Homing 安卓端 v2 任务

项目：/Users/fanqiege/Projects/agent-push-poc（android/ 目录，当前 0.6.2，versionCode 8）。项目已定名 Homing，图标选定「灰鸽 A」。请按下面的说明重做安卓端的品牌、界面和鸽子动效。

## 0. 先存档
把本消息原样保存为 docs/homing-v2/SPEC.md；把末尾的 JSON 保存为 docs/homing-v2/pigeon.json（格式化缩进）。之后按 SPEC 实现。

## 约束
- 不改 relay/server.mjs（中继由另一个 agent 负责）；只用现有 API。缺的字段（已读、置顶、静音、归档）先存在 App 本地，字段名用 read / pinned / muted / archived，之后中继会补上。
- 现有功能全部保留：回复、上传、模型和思考强度切换、停止、授权、富内容、CrashReporter。
- applicationId 暂不改（仍为 dev.agentpush.poc），避免用户重装后要重新设置 ColorOS 自启动和后台权限；只改显示名和图标。
- 不要向任何真实会话发测试消息；自测只用 /tmp 下的 Codex 测试线程。
- 不要在汇报里贴 token 或会话内容。

## 1. 品牌
- 应用显示名：Homing。
- 启动器图标（adaptive icon）：背景 #0F172A；前景是灰鸽，四层路径如下（坐标为 120×120 画布，整只鸽子先平移 (4,6)），缩放后放进 108dp 画布中间 66dp 安全区：
  - 身体轮廓 FULL：M96 41 L87 37 C86 28 74 24 68 30 C64 34 64 42 62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C84 47 85 44 88 43 Z，填色 #E2E8F0
  - 颈圈：pigeon.json 里 layers.neck.d，#2DD4BF
  - 翅膀：layers.wing.d，#94A3B8
  - 眼睛：圆心 (78,34)，半径 2.4，#0F172A
- 单色主题图标（monochrome，Android 13+）和通知栏小图标：只用 FULL 轮廓，白色，不画颈圈和翅膀。
- 字体：界面用系统无衬线；模型名、分支、计时、命令等等宽内容打包 JetBrains Mono（OFL 许可）。

## 2. 配色和规范（深色）
- 底 #0F172A；卡片 #1B2336；凸起 #263043；控件描边 #475569；底部导航底 #0B1222，上边线 #1E293B。
- 文字 #F8FAFC；次要 #94A3B8；正文浅灰 #CBD5E1。
- 品牌青 #2DD4BF（其上文字 #0F172A）；等你处理 琥珀 #FBBF24；出错 #F87171；diff 新增 #4ADE80。
- 圆角：卡片 12dp，按钮 10dp，FAB 16dp。
- 触控目标 ≥ 48dp；文字对比度 ≥ 4.5:1；不用 emoji 当图标；主内容不做只能横滑的操作（任何滑动操作都要有按钮替代）；尊重系统「减少动态」。

## 3. 界面（底部 3 个标签：收件箱 / 项目 / 我的）
**启动页**：居中大鸽子 148dp 播「归巢」后转「待机」；其下 Homing（44sp，600 字重，1.1 s 后淡入）和「每个 agent 的回复，都能飞回你手里。」（15sp 次要色，1.3 s 后淡入）；底部一行：26dp 小鸽子「扑翅」+「正在连接 我的 Mac…」，下面等宽小字「局域网 · 中继端口 18787」。连上后进入收件箱。

**收件箱**（默认首页）：
- 顶栏：28dp 鸽子 + Homing（18sp 600）+ 右侧「我的 Mac」连接状态胶囊（青点=已连，琥珀=重连中）+ 48dp 搜索按钮。每来一条新消息，顶栏鸽子「点头」一次。
- 大标题「N 件事等你」（28sp）+ 小字「另有 M 个在运行」。
- 运行中横条：每个运行中的会话一个 40dp 胶囊，「agent · 项目」+ 等宽计时，点进会话。
- 卡片按优先级：请求授权（琥珀盾牌图标、标题、等宽「Agent · 项目 · 分支」、命令代码块、拒绝/允许两个 48dp 按钮）→ 等你回复（问题原文、卡内直接回复输入框 + 语音按钮）→ 出错（红 ×、标题、红色原因、重试）。
- 「刚完成」小节：对勾、标题、等宽「agent · 项目 · 改了 N 个文件」、未读青点；右侧「全部已读」。
- 右下 56dp 青色 FAB「新任务」；底部导航收件箱标签上琥珀角标显示待处理数。
- 全部处理完时（空状态）：标题「都处理完了」+「M 个在运行」；中间 112dp 鸽子「待机」、「没有等你处理的事」、「agent 需要你回复或授权时，会出现在这里，同时推送到通知栏。」；下面仍显示「刚完成」，右侧链接改为「全部会话」。

**项目**：分段控件「按项目 / 按时间」；agent 筛选胶囊；项目卡片（项目名、叠放的 20dp agent 小头像、等宽「分支 · 会话数 · 最近时间」）下列会话行（标题、agent、状态点、时间）。空会话不显示；/tmp 等临时目录的会话折叠到最底部「临时会话 N 个」。

**会话**：顶栏返回 + 会话标题 + 等宽一行「Codex · agent-push-poc · main」；运行中显示计时和停止按钮；上下文用量细条（used/limit）。内容：用户气泡（带「模型 · 强度」小字）、思考折叠为「思考 12s」可展开、工具卡（名称、等宽输入摘要、输出末尾、退出码、状态）、文件改动卡（每个文件 +/− 行数，展开看 diff）、计划清单。底部：模型胶囊 + 强度胶囊（点开模型面板）、输入框、发送/停止。

**模型面板**（底部弹出）：标题「模型」，副标题「只对下一条消息生效」；按提供方分组的单选列表，标出「当前」「默认」；模型支持时显示强度分段「最低 / 低 / 中 / 高」；底部按钮「用于下一条消息」。

**我的**：Mac 连接卡（地址、状态、重新配对）；agent 列表（Codex、Claude Code、WorkBuddy、OpenCode、ZCode，显示已接入/待接入）；提醒开关（等你回复、请求授权、出错、完成）；底部鸽子 + 「Homing 0.7.0」。

## 4. 鸽子动效
做一个 Compose 组件 HomingPigeon(state, size, showTile, showEye, showWing)，用 PathParser 把 pigeon.json 的路径转成 Path，在 Canvas 里按层绘制，每层用 withTransform 绕支点平移/旋转/缩放。分组：bird ⊃ squash ⊃ [body, head ⊃ eye, neck, wing]。
- idle：4.8 s 循环。点头两下 → 停 → 抬一下翅 → 眨眼；颈圈颜色在 #2DD4BF 和 #5EEAD4 之间流动。
- flap：0.56 s 循环。翅膀绕肩 (72,68) 旋转到 60° 并拉长 1.3×1.1，再下拍到 −6°；整只随之上下起伏。
- arrive：1.6 s 播一次。从 (−70,−58)、−10°、0.55 倍、透明飞入，扑三下翅，落地时绕脚 (52,90) 压扁 1.07×0.91 再回弹，点一次头，然后转入 idle。
- bob_once：快速点两下头，共 0.8 s，不循环，用于顶栏「新消息到了」。
- static：系统动画时长缩放为 0（Settings.Global.ANIMATOR_DURATION_SCALE == 0）时，所有位置都用静态图标。
关键帧时间（秒）和数值以 pigeon.json 为准；keys 里没写的字段取默认值（tx/ty/rot 为 0，sx/sy 为 1，alpha 为 1）；正角度表示翅膀抬起（y 轴向下）。
使用位置：启动页（arrive→idle，底部小鸽子 flap）、收件箱顶栏（新消息 bob_once）、收件箱空状态（idle）、下拉刷新和断线重连（flap，连上即停）。启动器图标和通知栏图标永远静止。

## 5. 交付
- ./gradlew assembleDebug 通过；版本号 0.7.0（versionCode 9）；APK 按以前的方式放到中继 /apk 能下载的位置。
- README 里把名字改成 Homing，加一句简介「Every coding agent, one pocket inbox.」。
- 完成后汇报：改了哪些文件、哪些没做完、需要中继补哪些字段。

## pigeon.json
{"canvas":"120x120 viewBox; whole bird group pre-translated by (4,6); draw order body, head(+eye), neck, wing","tile":{"rx":27,"fill":"#0F172A"},"layers":{"groups":"bird ⊃ squash ⊃ [body, head ⊃ eye, neck, wing]","bird":{"pivot":[0,0],"note":"outermost group: fly-in translate/rotate/scale and flap hover, about bird origin"},"squash":{"pivot":[52,90],"note":"wraps all layers; landing squash about the feet"},"body":{"d":"M62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C82 44 70 42 62 48 Z","fill":"#E2E8F0"},"head":{"d":"M66 38 C65 28 77 24 84 29 C86 31 87 34 87 37 L96 41 L88 43 C86 47 80 50 73 50 C68 49 66 44 66 38 Z","fill":"#E2E8F0","pivot":[0,0],"note":"eye is a child of head"},"eye":{"ellipse":{"cx":78,"cy":34,"r":2.4},"fill":"#0F172A","pivot":[78,34]},"neck":{"d":"M63 44 C70 50 80 51 86 49 L87 55 C79 57 68 55 61 50 Z","fill":"#2DD4BF"},"wing":{"d":"M36 71 C50 61 66 61 76 68 C64 80 48 82 36 71 Z","fill":"#94A3B8","pivot":[72,68],"note":"rotate then scale about shoulder; positive deg lifts wing (y-down)"}},"static_full_silhouette":"M96 41 L87 37 C86 28 74 24 68 30 C64 34 64 42 62 48 C56 56 44 60 30 66 L12 74 L31 79 C40 89 58 94 74 88 C92 80 96 60 86 50 C84 47 85 44 88 43 Z","easing":{"default":"cubic-bezier(.45,0,.55,1) per segment","arrive_bird":"cubic-bezier(.2,.7,.3,1)","squash":"ease-out","eye":"linear"},"states":{"idle":[{"layer":"head","period_s":4.8,"delay_s":0,"loop":true,"keys":[[0,{"tx":0.0,"ty":0.0}],[0.22,{"tx":3.0,"ty":1.5}],[0.45,{"tx":0.0,"ty":0.0}],[0.67,{"tx":3.0,"ty":1.5}],[0.9,{"tx":0.0,"ty":0.0}],[4.8,{"tx":0.0,"ty":0.0}]]},{"layer":"eye","period_s":4.8,"delay_s":0,"loop":true,"keys":[[0,{"sx":1.0,"sy":1.0}],[3.36,{"sx":1.0,"sy":1.0}],[3.46,{"sx":1.0,"sy":0.12}],[3.58,{"sx":1.0,"sy":1.0}],[4.8,{"sx":1.0,"sy":1.0}]]},{"layer":"wing","period_s":4.8,"delay_s":0,"loop":true,"keys":[[0,{"rot":0.0}],[2.1,{"rot":0.0}],[2.3,{"rot":8.0}],[2.48,{"rot":-2.0}],[2.65,{"rot":0.0}],[4.8,{"rot":0.0}]]},{"layer":"neck","period_s":4.8,"delay_s":0,"loop":true,"keys":[[0,{"color":"#2DD4BF"}],[2.4,{"color":"#5EEAD4"}],[4.8,{"color":"#2DD4BF"}]]}],"flap":[{"layer":"wing","period_s":0.56,"delay_s":0,"loop":true,"keys":[[0,{"rot":0.0}],[0.252,{"rot":60.0,"sx":1.3,"sy":1.1}],[0.42,{"rot":-6.0}],[0.56,{"rot":0.0}]]},{"layer":"bird","period_s":0.56,"delay_s":0,"loop":true,"keys":[[0,{"tx":0.0,"ty":0.0}],[0.252,{"tx":0.0,"ty":1.5}],[0.42,{"tx":0.0,"ty":-2.5}],[0.56,{"tx":0.0,"ty":0.0}]]},{"layer":"neck","period_s":1.12,"delay_s":0,"loop":true,"keys":[[0,{"color":"#2DD4BF"}],[0.56,{"color":"#5EEAD4"}],[1.12,{"color":"#2DD4BF"}]]}],"arrive_then_idle":[{"layer":"bird","period_s":1.6,"delay_s":0,"loop":false,"keys":[[0,{"tx":-70.0,"ty":-58.0,"rot":-10.0,"sx":0.55,"sy":0.55,"alpha":0.0}],[0.16,{"tx":-58.0,"ty":-48.0,"rot":-9.0,"sx":0.6,"sy":0.6,"alpha":1.0}],[1.0,{"tx":0.0,"ty":0.0,"rot":0.0,"sx":1.0,"sy":1.0,"alpha":1.0}],[1.6,{"tx":0.0,"ty":0.0,"rot":0.0,"sx":1.0,"sy":1.0,"alpha":1.0}]]},{"layer":"squash","period_s":1.6,"delay_s":0,"loop":false,"keys":[[0,{"sx":1.0,"sy":1.0}],[1.0,{"sx":1.0,"sy":1.0}],[1.1,{"sx":1.07,"sy":0.91}],[1.24,{"sx":0.98,"sy":1.03}],[1.36,{"sx":1.0,"sy":1.0}],[1.6,{"sx":1.0,"sy":1.0}]]},{"layer":"wing","period_s":1.6,"delay_s":0,"loop":false,"keys":[[0,{"rot":55.0,"sx":1.3,"sy":1.1}],[0.15,{"rot":-4.0}],[0.32,{"rot":60.0,"sx":1.3,"sy":1.1}],[0.48,{"rot":-4.0}],[0.64,{"rot":60.0,"sx":1.3,"sy":1.1}],[0.8,{"rot":-4.0}],[0.94,{"rot":35.0,"sx":1.15,"sy":1.05}],[1.08,{"rot":0.0}],[1.6,{"rot":0.0}]]},{"layer":"head","period_s":1.6,"delay_s":0,"loop":false,"keys":[[0,{"tx":0.0,"ty":0.0}],[1.3,{"tx":0.0,"ty":0.0}],[1.42,{"tx":3.0,"ty":1.5}],[1.56,{"tx":0.0,"ty":0.0}],[1.6,{"tx":0.0,"ty":0.0}]]},{"then":"idle","after_s":1.6}],"bob_once":[{"layer":"head","period_s":0.8,"delay_s":0,"loop":false,"keys":[[0,{"tx":0.0,"ty":0.0}],[0.2,{"tx":2.5,"ty":1.2}],[0.4,{"tx":0.0,"ty":0.0}],[0.6,{"tx":2.5,"ty":1.2}],[0.8,{"tx":0.0,"ty":0.0}]]}],"static":"no animation (reduced motion / ANIMATOR_DURATION_SCALE == 0 / launcher & notification icons)"}}
