---
name: rhino-mcp-modeling
description: 安装和配置 Rhino MCP 插件与连接，并通过 Rhino MCP server 在 Windows 或 macOS Rhino 8 中逐步建模、修改、检查和重放几何操作。
---

# Rhino 建模

## 1. 建模规范

- 先确认活动文档、单位、现有几何和用户目标。默认毫米；选中部件解析成唯一稳定名称后再修改，重放不依赖选择集、临时 GUID 或 Python 全局状态。
- 一次工具调用只改变一个特征或一个部件，完成后 `doc.Views.Redraw()`；孔、柱、紧固件分别推进。先构建并检查有效性、闭合性及布尔结果数量，再按名称替换目标，不清空整个文档。
- 需要重复运行时，从已记录的原始基准重建，不能累计缩放或平移。共用 helper 或几何快照必须由可重放的 setup 保存，后续检查版本/哈希和前置几何。
- 实际器件、安装孔和壁厚不随整体比例任意变化。先建立清晰主形，控制点按形状需要增加；简化曲面后检查偏差和连续性。
- 审阅期间保留独立可编辑部件。用户确认后才合并应成为同一打印件的部分；运动件、采购件及参考体保持分离。局部修改原有单件不算与其他部件合并。
- 在薄壁、承力连接或姿态变化后，检查孔槽配合、实体干涉、脚垫接触及必要运动位置。功能打印可注明 PETG 假设，保留合理根部过渡和连续受力截面；几何通过不等于强度、全行程或实物验证。
- `script` 用于 Python/RhinoCommon 建模；`exec` 仅用于完整非交互宏；`capture` 只在有具体视觉问题时使用。全黑截图不作验证证据，不能假设 Python `print` 会随 MCP 返回，数值检查用断言。
- 失败或超时后先检查现场，再修正；不盲目重复动作。操作说明和尺寸变化同步写入任务 Markdown。

## 2. 安装、配置与调用

交付根目录就是本 `SKILL.md` 所在的 `rhino-mcp-modeling` 文件夹。以下路径均相对此目录，调用前解析为本机绝对路径，不依赖终端工作目录：

| 文件 | 用途 |
| --- | --- |
| Windows：`bin/rhino-mcp-server.exe`；macOS：`bin/rhino-mcp-server` | MCP stdio server |
| Windows：`bin/rhino-tool.exe`；macOS：`bin/rhino-tool` | 独立 config / replay 工具 |
| `bin/rhinomcpplugin-*.yak` | Rhino 插件的 Yak 安装包，Windows/macOS 压缩包均包含 |
| `.env.example` | 兼容 MCP server 的配置模板；Rhino 插件不读取 `.env` |

### 安装和初始化对话流程

用户说“安装 Rhino MCP 插件”“设置 Rhino MCP”“初始化 Rhino MCP 插件配置”或意思相近时，第一问必须是：“Rhino 与当前运行本 skill/MCP server 的软件是同一台电脑，还是不同电脑？”。在得到回答前，不安装、不读取配置，也不同时询问其他信息。

后续按以下顺序进行，并且每一轮只问一个问题，等待回答后再问下一项；不要一次向用户索取安装状态、重启状态、配置内容和地址：

1. 先根据第一问选择安装位置。必须使用当前 skill `bin/` 下唯一的 `rhinomcpplugin-*.yak`，不要安装散装 `.rhp`；缺失或存在多个候选时停止并明确报告。
2. 同一台电脑：解析 `.yak` 的实际绝对路径和本机 Rhino Yak 路径，然后执行安装。
3. 不同电脑：这一轮只询问 Rhino 8 的安装目录，要求提供包含 `System` 子目录的根目录，例如 `C:\Program Files\Rhino 8`。收到后，下一轮只询问准备把 `.yak` 放到 Windows 电脑的哪个目录。不得假定 Rhino 安装位置、用户目录、下载目录或盘符。
4. 收齐两个目录后，从当前 skill `bin/` 解析 `.yak` 的实际文件名，用用户提供的路径拼出完整 PowerShell 命令：`& "<Rhino安装目录>\System\Yak.exe" install "<Yak放置目录>\<实际Yak文件名>"`。命令中不得保留“实际路径”等占位符；路径必须加引号。给出复制 `.yak` 的来源绝对路径及最终命令，然后只询问命令执行后是否显示安装成功。
5. 安装成功后只询问“Rhino 是否已经完全退出并重启？”。用户确认前不继续读取配置。Rhino 需要 8.19 或更新版本。
6. 同一台电脑且已重启：不要让用户复制配置。自行运行 `bin/rhino-tool config`，读取用户目录 `.rhino-mcp/plugin-port-*.json` 或 `.rhino-mcp/config.json`，随后用带 token 的 `/health` 检查连接；输出中不得显示 token。
7. 不同电脑且已重启：给出 Rhino 所在 Windows 电脑的配置路径 `%USERPROFILE%\.rhino-mcp\config.json`（通常为 `C:\Users\<Windows用户名>\.rhino-mcp\config.json`），这一轮只请用户复制并粘贴该文件的完整 JSON。收到后校验它只需包含合法的 `port` 和至少 32 字符的 `token`，不得回显 token。下一轮再单独询问 Rhino Windows 电脑的 IP 地址或主机名。
8. 收齐远程配置和地址后，在本 skill 根目录写入 `.env`：`RHINO_HOST` 使用用户提供的地址，`RHINO_PORT` 和 `RHINO_TOKEN` 使用用户粘贴的配置。收紧文件权限并运行 `bin/rhino-tool config` 与 `/health` 验证。若 MCP server 所在电脑已有 `.rhino-mcp/plugin-port-*.json` 或 `config.json`，它们会优先于 `.env`；先说明冲突，再单独询问是否将本地配置改名停用，不要静默删除。

`config.json` 不接受远程地址；其中的 `addresses` 即使存在也会被忽略。`addresses` 只会出现在插件生成的活动端点文件中。远程主机必须通过 skill 根目录 `.env` 的 `RHINO_HOST` 设置。

Windows 包为 x64；macOS 程序为 Universal（Apple Silicon/Intel）。插件为 AnyCPU，以 `.yak` 安装包交付。安装命令如下，将包路径替换为 `bin/` 中解析出的绝对路径：

Windows PowerShell 命令结构（交互流程中必须先收集两个目录，再替换成完整实际路径）：
```powershell
& "<Rhino安装目录>\System\Yak.exe" install "<Yak放置目录>\<实际Yak文件名>"
```

macOS 终端：
```sh
"/Applications/Rhino 8.app/Contents/Resources/bin/yak" install "/实际路径/rhinomcpplugin-1.0.0-rh8_19-any.yak"
```

安装方式见 [Yak 官方命令说明](https://developer.rhino3d.com/guides/yak/yak-cli-reference/)。插件只使用 Rhino 所在机器用户目录的 `.rhino-mcp/config.json`，缺少或无效时自动生成端口和随机 token。

保持整个 skill 文件夹一起移动；程序自动从 `bin` 定位根目录，非标准位置可设置 `RHINO_ROOT`。直接将 MCP server 注册到当前 AI 应用，无需额外 MCP client 程序。假设 skill 位于 `C:\Skills\rhino-mcp-modeling`：

```json
{"command":"C:\\Skills\\rhino-mcp-modeling\\bin\\rhino-mcp-server.exe","args":[]}
```

macOS 配置示例（将用户名和目录替换为实际绝对路径，JSON 中不使用 `~`）：

```json
{"command":"/Users/you/Skills/rhino-mcp-modeling/bin/rhino-mcp-server","args":[]}
```

server 和 tool 的连接顺序：先用户目录 `.rhino-mcp/plugin-port-*.json`（活动实例，最新优先），其次同目录 `config.json`（持久化 port/token），均不存在时再读交付根目录 `.env`。用户目录在 Windows 为 `%USERPROFILE%`，macOS 为 `$HOME`。本地配置固定连接 127.0.0.1；进程连接环境变量不覆盖此顺序。损坏或失联的本地配置不能自动切换到远端。

插件启动只使用 `config.json`；没有或无法读取时可迁移已退出实例的端点配置，都没有则生成端口和随机 token。首次启动将最终配置持久化，并发布当前实例端点。端口冲突时使用临时端口/新 token，不覆盖已有持久配置；正常退出删除实例文件，保留 `config.json`。修改持久配置后重启 Rhino。

插件配置使用用户目录 `.rhino-mcp/config.json` 中的 `port` 和至少 32 字符的 `token`；缺少或无效时由插件自动生成兜底配置。Windows 用 `& '<skill绝对路径>\bin\rhino-tool.exe' config`，macOS 用 `'<skill绝对路径>/bin/rhino-tool' config` 查看命中路径，不显示 token。

通过已注册的 `script` 工具直接发送代码，`journal` 字段指定模型、步骤说明与类型。例如工具参数：

```json
{
  "lang":"python",
  "code":"import Rhino\ndoc=Rhino.RhinoDoc.ActiveDoc\nassert doc is not None",
  "journal":{"model":"frame","label":"检查活动文档","kind":"check"}
}
```

三个工具均支持 `journal`；`kind` 为 `model/check/view`，可补充 `note` 和 `replaces`。不传时生成会话日志。旧客户端的 `_meta["rhino-mcp/journal"]` 仍兼容，两种方式不要同时传。服务端先持久化 request 再执行，随后记录结果；无需另写记录脚本或重复调用。

本 skill 的 `scripts/inspect.py` 可读取后作为 `script.code` 发送，不要把本机文件路径误当成 Rhino 主机路径。

## 3. Replay

记录固定输出到本 `SKILL.md` 同目录的 `models-logs\<model>.jsonl`，项目源码布局和发布包均遵循此规则。项目中为 `src/skills/rhino-mcp-modeling/models-logs`，发布包中为 `rhino-mcp-modeling/models-logs`。同一模型沿用同一名称；截图独立保存在 `.artifacts` 文件夹。不要改写历史行。

```powershell
$tool = 'C:\Skills\rhino-mcp-modeling\bin\rhino-tool.exe' # 替换为本 skill 的实际路径
& $tool replay --model frame --dry-run
& $tool replay --model frame --step 0.5
& $tool replay --log C:\Models\frame.jsonl --from STEP_ID
```

macOS 同等命令：

```sh
tool='/Users/you/Skills/rhino-mcp-modeling/bin/rhino-tool'
"$tool" replay --model frame --dry-run
"$tool" replay --model frame --step 0.5
"$tool" replay --log /Users/you/Models/frame.jsonl --from STEP_ID
```

tool 直接通过插件 HTTP 协议顺序执行，不启动 MCP server 或 MCP client。默认每步完成后间隔 0.5 秒；重放不追加源建模日志，截图和最近结果仍写入本 skill 的 `models-logs`。

先校验完整日志，再使用 `--from` 选择有效步骤；必须有完整 setup 或已核对的现场基准。不能跳过未解决的 `error/uncertain/pending`。成功且自包含的修正可用 `journal.replaces=["旧步骤ID"]` 替代先前步骤，不能仅为校验通过而标记替代。

只自动重试确定的连接失败或端点未就绪。超时、响应中断或脚本失败时停止，检查实际模型后再决定继续。发现 `.lock` 时，确认原进程和 Rhino 操作都已结束后才处理锁文件。
