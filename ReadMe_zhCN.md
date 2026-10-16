# wbtray 3（Rust）

用于 [workbuddy2api-panel 网关](https://github.com/linguo2625469/workbuddy2api-panel)
的 Windows 通知区域程序。显示网关和账号状态，打开网关网页面板，复制地址或密钥，
并管理由当前托盘实例启动的本地网关子进程。

[English](README.md) | **简体中文**

## 版本与功能范围

当前源码是 Rust 3.0.0 实现。已发布的 **v2.0.2 是另一套 Go 实现**，
它的截图、配置、安装更新行为和构建命令不适用于本源码。
Cargo.toml 中的 3.0.0 版本号不代表 Rust 版本已发布。

当前界面是带猫图标的 Windows 原生托盘菜单，菜单标签为中文。
账号配置和详细管理使用网关自己的网页面板。本实现没有自绘控制台、自动下载网关、
自更新、控制台窗口开关、主题选择或语言切换。

## 手动安装

1. 编译当前 Rust 源码，或从可信发行版取得明确标注为本 Rust 版本的二进制。
   不要把 v2.0.2 下载当作 Rust 3。
2. 把 `wbtray.exe` 放在可写目录中。
3. 从网关上游单独下载网关，解压到托盘旁的 `wb2api` 目录。
   按网关自己的说明初始化账号和配置。
4. 启动 `wbtray.exe`。配置不存在时会生成 `wbtray.conf`。

```text
wbtray/
  wbtray.exe
  wbtray.conf
  wb2api/
    wb2api.exe
    config.json
    auths/
    data/
```

托盘不会下载缺失的网关。若只观察已经运行的远程网关，不需要安装本地网关：
明确填写远程地址及密钥，并关闭网关联动。

## 配置

`wbtray.conf` 使用简单的 `key = value` 行格式，不是完整 TOML。
注释应单独占一行。支持的设置只有：

```text
base_url =
api_key =
autostart_gateway = true
autostart_tray = false
```

- `base_url` 留空：从本地网关 `listen` 配置读取主机与端口，通配监听使用同地址族的回环地址；
  无法取得端口时使用 `http://127.0.0.1:7863`。
- `api_key` 留空：只有被识别为本地安装对应端点时，才继承本地网关密钥。
  显式远程 URL 绝不会继承本地密钥；需要认证时请单独填写远程密钥。
- 每轮轮询重新读取 `wb2api/config.json`，刷新继承的地址、密钥和任务状态。
  继承得到的凭证不会写回托盘配置；显式填写的托盘设置优先。
- `autostart_gateway` 允许在本地端点不可用时启动已经安装的本地网关。
  菜单中的联动开关修改此项。
- 使用 **自启** 菜单注册或取消随 Windows 启动。实际机制是当前用户的 Run
  注册表项，单独修改配置标志不等于完成安装。启动命令的可执行路径带引号；
  已失效或指向其他程序的值不会被识别为已启用。

配置优先保存在托盘旁，目录不可写时改用 `%APPDATA%\wbtray\wbtray.conf`。
手动修改托盘配置后请重启托盘；轮询过程中热读取的是网关配置。

## 菜单与进程归属

右击托盘图标使用原生菜单：

- 账号、状态和积分：当前网关读数
- **界面**：打开网关网页面板、复制 API 地址或密钥
- **任务**：本地网关计划任务的只读状态
- **联动**：本地随启及可用的启动、关闭、重启操作
- **自启**、**关于**、**退出**：Windows 启动项、关于、退出托盘

只有当前托盘实例启动并持有句柄的子进程可以被它关闭或重启。
手动、计划任务、其他程序或另一个托盘实例启动的网关均只观察、不控制。
远程端点也只观察，不套用本地配置中的任务状态。
退出托盘会保留运行中的网关；再次启动托盘后，该网关属于外部进程。

托盘以 `wb2api` 为工作目录启动其中的 `wb2api.exe`。
启动时保留网关配置中的完整监听地址，包括绑定主机，不会根据 URL 端口把
回环绑定扩大为 `0.0.0.0`。托盘启动的网关不显示控制台窗口。
关闭操作是终止进程，不保证等待请求正常完成，请避开重要请求进行操作。

复制使用 Win32 Unicode 剪贴板接口。复制密钥会把它放入 Windows 剪贴板，
可能被已启用的剪贴板历史、同步或其他剪贴板读取程序获取，请按凭证保护。

## 构建与测试

使用 Rust stable 和 Windows 工具链。依赖版本记录在 `Cargo.lock` 中；
本程序使用第三方 crate，并非零依赖。

MSVC Windows 构建需要 Visual Studio Build Tools 的 C++ 工作负载和 Windows SDK。
在能找到 `link.exe`、`rc.exe` 的 Developer PowerShell 中执行：

```powershell
rustup target add x86_64-pc-windows-msvc
cargo build --release --locked --target x86_64-pc-windows-msvc
cargo test --release --locked --target x86_64-pc-windows-msvc
```

也可使用 GNU Windows 工具链：安装 MinGW-w64，包括链接器和 `windres`，并加入 `PATH`：

```powershell
rustup target add x86_64-pc-windows-gnu
cargo build --release --locked --target x86_64-pc-windows-gnu
cargo test --release --locked --target x86_64-pc-windows-gnu
```

生成文件为 `target/<所选目标>/release/wbtray.exe`。以下检查也请使用所选目标。

```powershell
cargo fmt --check
cargo clippy --all-targets --locked --target x86_64-pc-windows-gnu -- -D warnings
```

### 必须执行的 Windows 桌面验收

以下均是 **待执行的手动检查，不是已验证的结果**。
编译和单元测试不能证明交互式桌面的通知区域行为正常。
请使用可丢弃的本地测试网关和不含真实密钥的剪贴板测试文本。

- 右击打开菜单，子菜单、键盘导航、Esc 和点击外部关闭均正常；
  在轮询及网关不可用时重复检查。
- 复制地址、密钥并粘贴到本地编辑器，验证 Unicode、连续复制和剪贴板占用时的错误提示。
  下方显式启用的测试会覆盖剪贴板；复制失败时必须报失败，而非跳过后算通过。
- 启动本地子进程，仅对该子进程关闭、重启；确认外部启动和远程网关不可被关闭。
- 修改网关端口、密钥、任务状态，确认下一轮轮询更新，且继承密钥不写回 `wbtray.conf`。
- 在带空格的安装路径切换自启，检查 Run 值中的引号，并确认旧路径不会显示为已启用。
- 退出后图标消失、网关继续运行；重开托盘后只观察已有网关。
- 重启 Windows Explorer，检查图标恢复、右键菜单和复制命令；记录 Windows 版本和结果。

```powershell
cargo test --locked --target x86_64-pc-windows-gnu clipboard::tests::interactive_round_trip -- --ignored
```

## 许可证

MIT，见 [LICENSE](LICENSE)。

迁移提示：旧 Rust 构建可能已把发现的地址和密钥保存为显式覆盖值。
如需重新跟随网关，请自行清空 `wbtray.conf` 的 `base_url` 和 `api_key`。
程序无法可靠区分这些值的来源，因此保留现有显式值，不自动迁移密钥。

