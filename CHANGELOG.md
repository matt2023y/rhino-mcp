# 变更记录

## 2026-09-22 — 精简安装说明

- 将两份详细安装文档替换为根目录 `INSTALL.md`，中英文仅保留选 ZIP、交给 Codex 安装 skill、输入“配置 犀牛 MCP”并按提示完成的流程。
- 按要求将安装说明文件名统一为大写 `INSTALL.md`，同步更新 README 链接。
- README 更新为新文档入口，未改动或执行打包。

## 2026-09-22 — ZIP 中英文安装说明

- 新增独立文档 `README.zh-CN.md` 和 `README.en.md`，说明 Windows x64 / macOS Universal 选包、skill 安装、STDIO MCP 注册、Rhino 插件安装和同机/跨机器配置。
- 明确 ZIP 按 AI/MCP 电脑系统选择、远程 `.env` 的位置、本地配置优先级，以及当前插件接受的客户端 IPv4 范围。
- 根 README 增加文档入口；修正 `.env.example` 中插件会读取该文件的过时注释。TOML 示例和本地文档链接已验证。
- 按用户补充要求撤回说明文件的打包集成，保留独立文档，不再执行打包；此前生成的本地 ZIP 未删除或再次修改。

## 2026-09-22 — 补充 Git 忽略规则

- 补充本地环境配置、Rhino 连接配置、临时建模产物、Go 测试及覆盖率输出、Python 缓存和系统/编辑器临时文件的忽略规则。
- 保留现有构建目录、模型记录和 Yak 工具忽略规则，继续允许提交 `.env.example` 与模型日志目录的 `.gitkeep`。

## 2026-09-22 — 打包逻辑迁入 Makefile

- 将 `scripts/package/main.go` 的文件清单、临时目录组装和 ZIP 生成改为 Makefile 内的 shell 命令，移除 Go 打包程序。
- 新增 `package-assemble-windows` / `package-assemble-macos`，保留输出路径、唯一 Yak 校验、macOS 执行权限及组装失败时保留旧包的行为。
- Windows PowerShell 入口改为调用 Makefile，提前检查所需 shell 工具；README 同步说明 Windows 的 MSYS2 工具依赖。
- 打包回归测试改为调用实际 Makefile 目标，覆盖文件白名单、两端归档内容、执行权限和失败保护。
- 验证：`go test ./...` 通过，包含带空格路径及 ZIP 生成失败场景；Windows PowerShell 入口尚未在 Windows 实机验证。

## 2026-09-21 — Skill 安装与初始化引导

- Rhino MCP skill 新增安装、设置和初始化的逐问流程：第一问先确认 Rhino 与当前软件是同一台还是不同电脑，再从 skill `bin/` 使用 Yak 安装 `.yak` 并确认完全重启 Rhino。
- 同机连接由工具自行读取活动端点或 `config.json`；远程连接先提示 Windows 配置路径并逐项索取配置 JSON、机器地址，再写入 skill 根目录 `.env` 和验证健康状态。
- 明确 `config.json` 仅包含 port/token，`addresses` 不是输入配置；本地端点会覆盖远程 `.env`，发生冲突时须先说明并单独询问是否停用本地配置。
- 远程 Windows 安装分支改为逐个询问 Rhino 8 安装目录和 `.yak` 放置目录，再结合 skill 中的实际包名生成无占位符、带完整引号的 PowerShell 安装命令。

## 2026-09-21 — 插件仅通过 Yak 交付

- 移除 Windows/macOS skill 文件夹及 ZIP 中单独的 `.rhp`、macOS `.rhp` 目录和 `.deps.json`，插件仅以 `bin/*.yak` 交付；Yak 内部保留必要的程序集和依赖清单。
- 更新 README、插件说明和 skill 安装步骤，统一使用本地 Yak 安装命令后重启 Rhino。
- 已重新生成两个平台发布包；打包器测试通过，确认文件夹和 ZIP 无单独 `.rhp` / `.deps.json`，且内嵌 Yak 完整、其中程序集与原始构建产物一致。

## 2026-09-21 — Skill 压缩包包含 Yak 安装包

- Windows/macOS skill 文件夹和 ZIP 的 `bin/` 新增本次生成的 `.yak` 插件安装包，保留原有插件文件及依赖清单。
- Makefile 的两个平台打包入口自动执行 `package-plugin`；Windows PowerShell 打包脚本使用 Rhino 8 自带的 Yak（支持 `-Yak` 覆盖）。
- 当前 Yak 构建保存于 `.build/yak-package/`，打包器要求其中恰好一个 `.yak`，避免漏包或混入历史版本；补充归档内容和失败时保留旧包的验证。
- 已重新生成两个平台 ZIP；打包器测试、ZIP 和内嵌 Yak 完整性、内嵌插件一致性及 macOS 执行权限检查通过。Windows PowerShell 脚本未在本机执行验证。

## 2026-09-21 — Yak 插件包

- 新增 `plugin/manifest.yml` 和 `make package-plugin`，使用项目根目录的 Yak 打包现有 Release 插件，版本从程序集读取，默认平台为 `any`，输出到 `dist/yak/`。
- 使用临时目录限制包内容为 `.rhp`、`.deps.json` 和 manifest；支持覆盖 Yak 路径、平台和运行时目录，保留现有 skill 打包流程。
- 根目录的 macOS ARM64 Yak 已替换为 .NET 8 版本；打包使用 `DOTNET_ROOT` 或自动定位的 .NET 8，不再依赖临时安装的 .NET 10，插件继续使用原 .NET SDK 构建。
- 已用 Yak 0.15.1 / .NET 8 生成 `rhinomcpplugin-1.0.0-rh8_19-any.yak`；压缩包完整性、文件清单和包内插件与输入产物的一致性检查通过，尚未在 Rhino 内安装加载验证。

## 2026-09-21 — 插件配置只使用 config.json

- Rhino 插件移除 `.env` 读取路径；缺少、无法读取或格式无效的 `config.json` 会记录警告并回退到旧端点或动态端口和随机 token，不再阻止插件启动。
- 删除插件配置解析器中的 `.env` 逻辑，并同步 Windows/macOS Skill 与插件说明。

## 2026-09-21 — Rhino 插件启动加载

- 为 `RhinoMcpPlugin` 声明 `PlugInLoadTime.AtStartup`，确保 Rhino 8 重启时自动执行 `OnLoad` 并启动 MCP 监听器；重新编译 Release 插件。

## 2026-09-21 — Windows / macOS 独立发布包

- `dist/windows` 和 `dist/macos` 各自包含完整 skill 目录及 ZIP；新增 `package-windows`、`package-macos`，`package` 一次生成两个包。
- 编译中间产物统一到 `.build`。Windows 为 amd64 exe；macOS 合并 Intel/Apple Silicon 为 Universal 程序，并进行本地 ad-hoc 签名。macOS 构建需要系统 `lipo` 和 `codesign`。
- Rhino 插件改为 AnyCPU；Windows 在 `bin` 放 .rhp 文件，macOS 在 `bin` 放 .rhp 插件包目录及程序集。ZIP 保留 Unix 执行权限。
- Skill 增补 macOS 程序路径、配置目录、插件安装和 replay 用法；两端日志仍与 SKILL.md 同级。测试覆盖平台清单、权限和新编译目录定位。
- 已实际生成两套 ZIP；Go tests/vet、插件配置及 skill 校验通过。macOS 解压后的配置定位、MCP 初始化/工具列表、Universal 架构及签名校验通过；尚未在两端 Rhino 内加载插件验证。旧单平台生成产物已移出 dist，保留在临时备份目录。

## 2026-09-21 — 插件与日志目录调整

- 发布包的 Rhino 插件及 deps.json 移到 `bin`，与两个 Go exe 同目录。
- 日志目录统一命名为 `models-logs`，始终紧邻 `SKILL.md`：源码项目为 `skills/rhino-mcp-modeling/models-logs`，发布包为根目录下的 `models-logs`。
- 同步 config 输出、自动记录、replay、打包清单、忽略规则及 skill 文档。旧项目日志目录只有占位文件，已迁移该占位文件。
- 验证通过：Go tests/vet、插件配置路径测试、源码和发布 skill 校验、Windows/插件构建及 ZIP 完整性检查；已按新目录重新生成发布包。

## 2026-09-21 — 自包含 skill 发布包

- 新增 `make package` 和 Windows `scripts/package.ps1`：先构建 Windows amd64 server/tool 与 Rhino 插件，再按明确清单复制到 `dist/rhino-mcp-modeling` 并生成 ZIP。
- Skill 内包含 `bin` 二进制、插件、inspect 脚本、配置模板及空日志目录；SKILL.md 明确二进制相对路径、绝对路径调用方式和插件位置。实际配置、历史日志和源码不进入发布包。
- Go 工具及插件支持从安装后的 skill 目录定位 `.env`，运行记录仍写入根目录下 `skills/model-logs`，可从任意工作目录启动。
- 增加发布清单/ZIP 内容及缺失产物保留旧包测试，扩展独立 skill 路径探测测试。
- 验证通过：Go tests/vet、插件配置测试、Release 构建、源 skill/发布 skill 格式校验以及 ZIP 完整性检查。实际生成约 5.5 MiB 的 Windows x64 发布包；尚未在 Windows Rhino 中实机加载。

## 2026-09-21 — 按最终要求拆分

- 用 `rhino-mcp-server.exe` 提供三个 MCP 工具及自动记录；`rhino-tool.exe` 仅负责 config/replay，重放直接调用 Rhino HTTP 接口，移除额外 MCP client 实现。
- 将原 Rhino 插件源码整理到 `plugin/`。启动优先复用用户目录持久配置，缺席时迁移旧端点或读取 .env，仍无配置则生成。首次结果原子保存；冲突实例使用临时端口，不覆盖已有配置。
- Go 端依次读取活动端点、持久 config.json、交付根目录 .env；所有记录和重放产物继续统一到 `skills/model-logs`。
- Skill 收敛为建模规范、MCP server 配置和 Replay 三部分；工具参数公开 `journal`，可直接指定模型、步骤和替代关系。
- 基础 Makefile 和 PowerShell 脚本构建两个 Go 程序，增加插件构建及配置测试入口。测试覆盖独立重放、配置优先级、自动记录和插件配置持久化；未操作现场模型。
- 实际验证：Go 全部测试与 vet 通过；两个 Windows amd64 exe 构建成功；插件 Release 构建通过（0 错误、0 警告）；配置持久化/并发/解析测试通过；原 23 步日志离线校验及 skill 格式校验通过。尚未进行 Windows Rhino 真机加载测试。
