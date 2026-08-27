![BongoCat](https://socialify.git.ci/JMTeCHNoLOGY/BongoCat/image?custom_description=&description=1&font=Source+Code+Pro&forks=1&issues=1&logo=https%3A%2F%2Fgithub.com%2FJMTeCHNoLOGY%2FBongoCat%2Fblob%2Fmain%2Fsrc-tauri%2Fassets%2Flogo-mac.png%3Fraw%3Dtrue&name=1&owner=1&pattern=Floating+Cogs&pulls=1&stargazers=1&theme=Auto)

> [!IMPORTANT]
> 本仓库基于 [ayangweb/BongoCat v1.1.0](https://github.com/ayangweb/BongoCat/tree/v1.1.0) 二次开发，原项目作者为 [ayangweb](https://github.com/ayangweb)。本分支新增了通过房间码进行联网联机、多人透明窗口、输入事件同步及配套房间服务。原项目的版权声明与 MIT License 均完整保留在 [`LICENSE`](./LICENSE) 中。

<div align="center">
  <p>
    <a href="./LICENSE"><img src="https://img.shields.io/github/license/JMTeCHNoLOGY/BongoCat?style=flat-square" alt="MIT License" /></a>
    <a href="https://github.com/JMTeCHNoLOGY/BongoCat/releases/latest"><img src="https://img.shields.io/github/package-json/v/JMTeCHNoLOGY/BongoCat?style=flat-square" alt="Version" /></a>
  </p>
</div>

## 项目说明

BongoCat 是一款基于 Vue 3、Tauri 和 Live2D 的跨平台互动桌宠，支持 macOS、Windows 和 Linux（X11）。猫咪会根据键盘、鼠标或手柄输入播放对应动作。

本分支在上游 `v1.1.0` 的基础上加入联网联机能力，并将自身版本线重置为 `1.0.0`。

## 功能

- 根据键盘、鼠标或手柄输入同步猫咪动作。
- 支持导入自定义 Live2D 模型。
- 支持通过房间码创建或加入联机房间。
- 在独立透明多人窗口中同步房间成员的猫咪与输入动作。
- 联机服务仅转发当前房间事件，不保存输入历史或皮肤资源。
- 单机模式默认离线运行，仅在明确加入房间后上传输入事件。

## 下载

构建产物发布在 [GitHub Releases](https://github.com/JMTeCHNoLOGY/BongoCat/releases)。如果当前尚无可用版本，请按照下方开发说明从源码运行。

每个版本同时提供以下安装包：

- macOS：ARM64、x86_64（DMG）
- Windows：ARM64、x86_64（NSIS EXE）
- Linux：ARM64、x86_64（DEB、RPM）

发布包未进行 Apple Developer ID 或 Windows 商业证书签名，系统首次运行时可能显示安全提示。每个 Release 都附带 `SHA256SUMS.txt` 校验文件。

## 本地开发

前端与桌面应用命令在仓库根目录运行：

```bash
pnpm install
pnpm tauri dev
```

仅启动前端页面：

```bash
pnpm dev
```

## 联机开发

先启动房间服务：

```bash
cd server
go run ./cmd/bongocat-server
```

然后在应用设置页的“联机”栏目创建或加入房间。默认服务地址为 `ws://127.0.0.1:8080/v1/ws`；远程部署必须通过反向代理提供 WSS。完整的服务端参数与容器部署说明见 [`server/README.md`](server/README.md)。

## 测试与构建

```bash
pnpm test
pnpm build
cargo test --manifest-path src-tauri/Cargo.toml
cd server && go test ./...
```

维护者可在版本号和提交准备完成后运行 `pnpm release:github`。脚本会推送对应版本标签，由 GitHub Actions 并行构建所有平台与架构，并在全部产物验证成功后公开 Release。

## 贡献

问题与建议请提交到 [GitHub Issues](https://github.com/JMTeCHNoLOGY/BongoCat/issues)。参与开发前请阅读[贡献指南](.github/CONTRIBUTING.md)。

## 许可证与署名

本项目继续使用 [MIT License](./LICENSE)。原始代码版权归 ayangweb 所有；使用、修改或分发本项目时，请保留原版权声明与许可证文本。

- 上游项目：[ayangweb/BongoCat](https://github.com/ayangweb/BongoCat)
- 本分支基准版本：[ayangweb/BongoCat v1.1.0](https://github.com/ayangweb/BongoCat/tree/v1.1.0)
