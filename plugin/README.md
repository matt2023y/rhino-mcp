# Rhino 8 插件

源码从本项目原 Plugin 整理而来，保留 HTTP /mcp、Bearer token、UI 线程串行执行和视口截图。

- 构建：在 src 执行 `make plugin` 或 `dotnet build plugin/RhinoMcpPlugin.csproj -c Release`。
- 配置测试：`make plugin-test`，无需安装 Rhino。
- 输出：`bin/Release/net7.0/RhinoMcpPlugin.rhp`。
- 程序集为 AnyCPU，供 Windows x64 和 macOS Intel/Apple Silicon 的 Rhino 8 使用。两个平台的 skill 发布包均通过 `bin/*.yak` 交付插件，`.rhp` 和依赖清单封装在 `.yak` 内部。
- 启动：用户 `.rhino-mcp/config.json` → 已退出实例的旧端点 → 生成端口/token。缺少或无法读取 `config.json` 时自动走兜底，不阻止插件加载；首次结果持久化，当前进程单独发布发现文件。
- 已有端口冲突时，临时端口使用新 token，原持久配置保留。关闭时清理进程文件，不删除 config.json。

配置实现位于 EndpointConfiguration.cs；全套使用及配置细节见上级 README。

## Yak 打包

在 `src` 目录执行 `make package-plugin`，使用根目录的 `yak` 打包现有 Release `.rhp` 和 `.deps.json`，输出到 `dist/yak/`。需要先重新编译时执行 `make plugin`。`plugin/manifest.yml` 的版本由 Yak 从程序集自动读取，作者字段沿用程序集的 Company 名称。

`make package-windows`、`make package-macos` 和 `make package` 会自动执行 Yak 打包，将本次生成的 `.yak` 放入对应 skill 文件夹和 ZIP 的 `bin/` 下。`.build/yak-package/` 只保存本次生成的安装包，历史版本不会混入 skill 压缩包。

默认 `YAK_PLATFORM=any`，供 Windows/macOS 的 Rhino 8 .NET 模式使用；最低 Rhino 版本由 Yak 根据程序集引用自动识别。可通过 `YAK=/absolute/path/to/yak`、`YAK_PLATFORM=win` 或 `YAK_PLATFORM=mac` 覆盖工具路径和平台。

当前根目录提供的 macOS ARM64 Yak 使用 .NET 8。Makefile 优先使用环境变量 `DOTNET_ROOT`，未设置时从 `dotnet --list-runtimes` 定位 .NET 8，也可设置 `YAK_DOTNET_ROOT=/path/to/dotnet`；不会改变编译插件所用的 `DOTNET`。打包在临时目录中进行，仅包含 manifest、插件和 deps.json，不包含源码、PDB、配置或 Go 程序，也不会发布到 Yak 服务器。

打包格式参考 [Rhino 官方 Yak 插件打包文档](https://developer.rhino3d.com/en/guides/yak/creating-a-rhino-plugin-package/)。
