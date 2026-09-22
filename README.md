需要自己下载 [.NET 8 版本的 yak cli程序](https://developer.rhino3d.com/guides/yak/yak-cli-reference/) 并授权, 直到可以运行 yak 不报错;

# Rhino MCP — Windows / macOS

本目录可单独复制使用，包含两个 Go 程序、Rhino 插件源码和精简建模 skill。

安装与配置见 [INSTALL.md（中文 / English）](INSTALL.md)。

```text
src/
├── .build/                    windows / macos 中间编译产物（不提交）
├── dist/                      windows / macos 各自的 skill 文件夹和 ZIP
├── cmd/
│   ├── rhino-mcp-server/
│   └── rhino-tool/
├── internal/rhino/             共享配置、插件通信、日志实现及测试
├── plugin/                    C# Rhino 8 插件源码与配置测试
├── scripts/build.ps1          Windows Go 构建脚本
├── Makefile
├── .env.example
└── skills/
    └── rhino-mcp-modeling/
        ├── SKILL.md           建模规范、MCP server 配置、Replay
        ├── scripts/inspect.py
        └── models-logs/       记录、截图、重放结果，与 SKILL.md 同级
```

正常建模：AI 应用 → Go MCP server → Rhino 插件。重放：Go tool → Rhino 插件。程序运行无需 Node.js 或另外的 MCP client。

## 配置与插件启动

本地配置目录在 Windows 为 `%USERPROFILE%\.rhino-mcp`，在 macOS 为 `~/.rhino-mcp`：

| 文件                          | 用途                                                  |
| ----------------------------- | ----------------------------------------------------- |
| `config.json`                 | 持久化 `port`、`token`，跨 Rhino 重启保留             |
| `plugin-port-<pid>-<id>.json` | 当前实例实际端口、token、进程号及地址，正常关闭时删除 |

插件启动首先读取持久配置；不存在或无法读取时尝试迁移已退出实例的端点配置；仍没有时使用系统分配端口并生成随机 token。监听成功后原子保存首份 `config.json`，每个实例单独发布端点文件。已有配置不被后启动实例覆盖；端口被占用时，新实例改用动态端口和新 token。插件不读取 `.env`，缺少或损坏的 `config.json` 不阻止启动，会走动态配置兜底。

Go server/tool 优先读取最新的活动端点，其次 `config.json`，都不存在才读取交付根目录 `.env`。本地文件固定指向 127.0.0.1。无效或无法连接的本地端点不自动改连另一台机器；检查 Rhino 状态后处理。Go 程序不读取旧 `rhino.local.mk`，也不以进程连接环境变量覆盖文件。

需要后备连接时，从 `.env.example` 复制为 `.env`，一起填写 `RHINO_HOST`、`RHINO_PORT`、`RHINO_TOKEN`；token 至少 32 字符。可选 `RHINO_REQUEST_TIMEOUT_MS`，默认 120000，上限 600000。`.env` 支持 BOM、CRLF、引号和注释，不执行内容、不展开变量。只有 MCP server/tool 读取 `.env`；插件从 Rhino 用户目录的配置读取端口/token，并监听允许的本地/LAN 地址。

开发产物位于 `.build/windows` 和 `.build/macos`；发布程序和插件位于 skill 的 `bin` 下。程序从自身路径定位根目录，路径不标准时设置 `RHINO_ROOT`。真实配置和旧模型记录不会复制进发布包。

## 打包 skill

在仓库根目录运行以下命令；已经位于本目录时省略 `-C src`：

```sh
make -C src package-windows   # Windows x64
make -C src package-macos     # macOS Universal：Intel + Apple Silicon
make -C src package           # 两个平台
```

需要 Go 1.22+、兼容 net7.0 的 .NET SDK、Yak，以及 `make`、POSIX shell、`zip` 和常用文件命令（`mktemp/cp/chmod/mv/rm`）；当前根目录 Yak 使用 .NET 8。macOS Universal 构建另外需要 macOS 的 `lipo`（Xcode Command Line Tools）和 `codesign`。Windows 可安装 MSYS2 工具并加入 PATH，再运行 `powershell -File .\scripts\package.ps1`，脚本构建后调用 Makefile 的 shell 打包命令；默认调用 Rhino 8 自带的 Yak，可用 `-Yak` 指定其他路径。

Makefile 打包会编译对应平台的两个 Go 程序，并将现有 Release 插件用 Yak 打包；插件源码有变动时先执行 `make plugin`。然后按文件清单复制并生成各平台的 ZIP。两个 ZIP 的插件均以 `bin/*.yak` 安装包交付，`.rhp` 和依赖清单仅保留在 `.yak` 内部。输出：

```text
dist/
├── windows/
│   ├── rhino-mcp-modeling-windows-amd64.zip
│   └── rhino-mcp-modeling/
│       ├── SKILL.md
│       ├── bin/               两个 .exe、*.yak
│       ├── scripts/inspect.py
│       ├── .env.example
│       └── models-logs/.gitkeep
└── macos/
    ├── rhino-mcp-modeling-macos-universal.zip
    └── rhino-mcp-modeling/
        ├── SKILL.md
        ├── bin/
        │   ├── rhino-mcp-server
        │   ├── rhino-tool
        │   └── rhinomcpplugin-<版本>-rh8_19-any.yak
        ├── scripts/inspect.py
        ├── .env.example
        └── models-logs/.gitkeep
```

按操作系统复制对应的整个 `rhino-mcp-modeling` 文件夹到目标 skill 目录；MCP 注册、插件安装及 replay 命令写在包内 `SKILL.md`。Rhino 插件和程序同放在 `bin`，`models-logs` 与 `SKILL.md` 同级。ZIP 保留 macOS 程序的执行权限。打包不包含真实 `.env`、历史日志、源码、PDB 或构建缓存。

`dist` 是可重新生成的输出，每次 package 会替换同名目录和 ZIP；使用时复制到安装目录，避免把运行记录保存在构建输出里。以下开发命令仍以 `src` 为工作目录。

单独打包插件执行 `make package-plugin`：`.yak` 输出到 `dist/yak/`，并在 `.build/yak-package/` 保存本次构建，供 skill 打包使用，避免混入历史版本。已有对应平台构建和 Yak 包时，执行 `make package-assemble-windows` 或 `make package-assemble-macos`，直接使用 Makefile 中的 shell 命令复制白名单文件并生成 ZIP，无需 Go 打包程序。缺失输入文件、Yak 数量不为一个或压缩失败时保留旧发布包。

## 构建

Go 1.22+，仅标准库。`make build` / `make build-windows` 构建 Windows amd64；`make build-macos` 构建 macOS Universal。Windows 也可使用：

```powershell
.\scripts\build.ps1
go test ./...
```

插件沿用原项目 RhinoCommon 8.19 与 net7.0，使用 AnyCPU 以兼容两个系统，用兼容 .NET SDK 构建：

```powershell
dotnet build .\plugin\RhinoMcpPlugin.csproj -c Release
dotnet run --project .\plugin\tests\ConfigTests.csproj
```

插件编译产物为 `plugin\bin\Release\net7.0\RhinoMcpPlugin.rhp`，通过 `make package-plugin` 封装为 `.yak` 后交付。使用 Rhino 自带的 `yak install <安装包绝对路径>` 安装，然后重启 Rhino；各平台命令见包内 `SKILL.md`。插件使用用户目录的持久 `config.json`，不依赖 `.env` 或当前工作目录。

其余基础命令：`make test`、`make plugin`、`make plugin-test`、`make config`、`make server`。可用 `DOTNET=/path/to/dotnet` 指定 SDK 命令；`config/server` 使用本机 Go 运行，两个平台均可执行。

## 配置 MCP server

当前 AI 应用的 MCP 配置使用实际绝对路径，server 不需要参数：

```json
{
    "command": "C:\\Skills\\rhino-mcp-modeling\\bin\\rhino-mcp-server.exe",
    "args": []
}
```

提供 `script`、`exec`、`capture` 三个工具，每个工具可附带：

```json
{ "journal": { "model": "frame", "label": "创建支撑柱", "kind": "model" } }
```

它属于工具参数的一部分；实际 Python 放在 `script.code`。未指定 journal 时生成会话名称。服务端自动记录，调用方不需要包装器。完整规范见 [SKILL.md](skills/rhino-mcp-modeling/SKILL.md)。

## 独立工具

```powershell
.\.build\windows\rhino-tool.exe config
.\.build\windows\rhino-tool.exe replay --model frame --dry-run
.\.build\windows\rhino-tool.exe replay --model frame --step 0.5
.\.build\windows\rhino-tool.exe replay --log C:\Models\frame.jsonl --from STEP_ID
```

macOS 开发产物使用 `./.build/macos/rhino-tool`，参数相同；发布包使用 `bin/rhino-tool`。对应 server 没有 `.exe` 后缀。

`config` 只显示配置来源、端点和日志目录，不显示 token、不修改 Rhino。`replay` 直接使用共享 HTTP 传输调用插件；仅有 tool 和配置即可重放，无须运行 MCP server。

输出统一在 `SKILL.md` 旁的 `models-logs`：源码项目为 `src/skills/rhino-mcp-modeling/models-logs`，发布包为 `rhino-mcp-modeling/models-logs`。`config` 会显示实际绝对路径。

- `<model>.jsonl`：先同步写入请求，再执行并追加 `ok/error/uncertain` 结果。
- `<model>.jsonl.artifacts/`：普通截图，日志只记录路径和 SHA-256。
- `<model>.jsonl.replay-artifacts/`：重放截图，以每次运行 ID 区分。
- `<model>.jsonl.last-replay.json`：最近一次重放状态、最后步骤和完成数。

重放先校验全日志及替代关系，失败或未知状态不能通过 `--from` 跳过；源记录不被重写。请求超时或响应中断不自动重发。模型操作失败后需检查实际几何，再使用带 `journal.replaces` 的自包含修正。

## 验证范围

Go 测试使用模拟插件、真实 MCP server 进程以及独立 tool，覆盖配置优先级、请求先落盘、截图、旧 JSONL、失败停止和移走 server 后重放。插件配置测试不依赖 Rhino，覆盖持久化、并发生成、路径查找和配置解析。Windows Rhino 中的实际加载和几何操作仍需在目标机器验证；本轮未修改现场模型。

本次两套发布包均已实际生成，Go tests/vet、插件配置测试、skill 校验及 ZIP 校验通过；macOS 包解压到新目录后，程序权限、配置定位、MCP 初始化及 tools/list、两种架构的 ad-hoc 签名校验通过。两个系统的 Rhino 插件加载和实际几何操作仍需在目标 Rhino 中验证。
