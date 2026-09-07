# Pskora 风格实验分支稳定检查点

- 日期：2026-09-07
- 分支：`feature/pskora-ui`
- 协议基线祖先：`64e29a220855958134fbcbd6920b5eea2d6fe576`
- 隔离规则：不得直接覆盖正式 `custom`；经后续真机回归后再决定合并。

## 当前用户验收

用户已确认 UI 与主要功能基本稳定，可进入边用边测阶段。当前已实测范围包括主页视觉、跨页连接控制、局域网共享真实上网；最新菜单透明效果与跨页测试错误显示已构建，仍按后续使用反馈继续修正。

## 已实现范围

- Pskora 风格主页、紧凑节点列表、固定连接区和悬浮底部导航。
- 圆形连接控制、成功态描边/呼吸、跨页面节点与连接指标。
- 当前会话累计流量移至连接区；节点条不再显示历史流量。
- 连通性测试成功、超时、脱敏错误及重试反馈，并绑定实际测试节点。
- 分组标题长按上浮菜单：编辑、复制订阅链接、确认删除。
- 局域网共享、认证、地址复制、共享与 Clash API 开关联动及真实连接来源聚合。
- `geoip/geosite` 构建资源门禁与首次启动缺失恢复。

## 最后静态验证产物

`F:/pskora-lab/NekoBoxYF-group-menu-glass-release-unsigned.apk`

- SHA-256：`81826537d3f85b6b521626653b4e86d8eca2188b394b7e423ce2303f8aadf90c`
- 变体：arm64-v8a F-Droid Release，未签名
- 构建：`BUILD SUCCESSFUL in 3m 46s`
- 单元测试：53，失败 0，错误 0

## 可复现构建注意事项

- `app/src/main/assets/sing-box` 被 `.gitignore` 忽略；构建前运行 `buildScript/lib/assets.sh` 生成数据库资源。
- CI 必须递归初始化 `hev-socks5-tunnel` 子模块并构建 `app/libs/libcore.aar` 与 HevTun，不能依赖本地忽略产物。
- 当前本地 UI 测试曾使用既有 `libcore.aar` 和临时 HevTun 产物；它们不进入 Git 提交。正式发布仍应由 GitHub Actions 从源码重建并以真实设备回归协议。
- 不提交测试签名、用户节点、订阅链接、UUID、密码、Token 或设备日志。

## 后续回归清单

1. `x365`、`#juzi`、`#fastup`、TunNet 与标准协议真实联网。
2. 局域网共享启停、认证、Clash API 联动及客户端列表。
3. 单/双列节点布局、分组上浮菜单、长名称与深色主题。
4. 连通性测试在首页及其他页面的成功、超时、错误和切换节点竞态。
5. Android 11 状态栏、滚动层级、底部导航和透明菜单。
