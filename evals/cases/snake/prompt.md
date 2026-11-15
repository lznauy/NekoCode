制作一个能在本机浏览器玩的贪吃蛇小游戏，原生 HTML/CSS/JavaScript，无第三方依赖、CDN 或构建步骤。入口 index.html，可用 python3 -m http.server 启动。

功能：12×12 棋盘；点击开始后按固定节拍移动；方向键控制，禁止直接反向；吃食物增加一格与1分，食物不能落在蛇身上；撞墙或自身后结束；支持暂停/继续及重新开始。重开恢复初始长度与0分，不能因为多次重开而叠加定时器。初始蛇长3，初始朝右，移动间隔200毫秒。页面显示分数、状态和操作说明，正确处理键盘默认滚动。

为便于无障碍与外部验收，棋盘使用DOM格子而非canvas：每格有data-x/data-y（坐标0..11）、data-state（empty/snake/food）；棋盘容器id=board。分数元素id=score，文本为整数；状态元素id=status，文本为ready/running/paused/over。按钮id分别为start、pause、restart。暂停按钮切换暂停/继续。ready时不移动，start进入running，restart直接开始新局。其他布局和视觉设计自主决定。

自行验证并提供启动说明。完成后给出实际完成项、验证结果和剩余问题；不要把未执行的检查说成通过。
