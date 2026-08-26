#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAURI_BIN="$ROOT_DIR/node_modules/.bin/tauri"
VITE_BIN="$ROOT_DIR/node_modules/.bin/vite"
VITEST_BIN="$ROOT_DIR/node_modules/.bin/vitest"
ESLINT_BIN="$ROOT_DIR/node_modules/.bin/eslint"
TSX_BIN="$ROOT_DIR/node_modules/.bin/tsx"
VERSION="$(node -e "console.log(require(process.argv[1]).version)" "$ROOT_DIR/package.json")"
BUILD_ID="$(date '+%Y%m%d-%H%M%S')"
OUTPUT_DIR="${BONGOCAT_OUTPUT_DIR:-$ROOT_DIR/release-local/v${VERSION}-${BUILD_ID}}"
WINDOWS_TARGET="x86_64-pc-windows-msvc"
WINDOWS_TOOLCHAIN_DIR="${BONGOCAT_WINDOWS_TOOLCHAIN:-$ROOT_DIR/.local-toolchains/windows}"
SKIP_CHECKS=0
TARGET=""
ARTIFACTS=()

usage() {
  cat <<'EOF'
用法：
  ./scripts/package-local.sh macos [--skip-checks]
  ./scripts/package-local.sh windows [--skip-checks]
  ./scripts/package-local.sh all [--skip-checks]

也可以通过 pnpm 调用：
  pnpm package:local macos [--skip-checks]
  pnpm package:local windows [--skip-checks]
  pnpm package:local all [--skip-checks]

说明：
  macos   生成当前 Mac 架构的 BongoCat.app 和 DMG
  windows 在 macOS 上交叉生成 Windows x64 NSIS 安装包
  all     同时生成以上所有包

环境变量：
  BONGOCAT_OUTPUT_DIR          自定义产物目录
  BONGOCAT_WINDOWS_TOOLCHAIN   自定义 Windows 交叉编译缓存目录

首次构建 Windows 包会在 .local-toolchains/windows 下载约 3 GB 工具链。
生成的是本地未签名包，适合测试和内部分发。
EOF
}

fail() {
  printf '错误：%s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "缺少命令 '$1'。${2:-}"
}

heading() {
  printf '\n==> %s\n' "$1"
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      macos|windows|all)
        [[ -z "$TARGET" ]] || fail '只能选择一个打包目标'
        TARGET="$1"
        ;;
      --skip-checks)
        SKIP_CHECKS=1
        ;;
      -h|--help)
        usage
        exit 0
        ;;
      *)
        fail "未知参数：$1"
        ;;
    esac
    shift
  done

  [[ -n "$TARGET" ]] || {
    usage
    exit 1
  }
}

run_checks() {
  if [[ "$SKIP_CHECKS" -eq 1 ]]; then
    heading '跳过测试与代码检查'
    return
  fi

  heading '运行前端测试与代码检查'
  "$VITEST_BIN" run
  "$ESLINT_BIN" src

  heading '运行 Rust 测试与格式检查'
  cargo fmt --manifest-path src-tauri/Cargo.toml -- --check
  cargo test --manifest-path src-tauri/Cargo.toml

  if command -v go >/dev/null 2>&1; then
    heading '运行多人服务器测试'
    (cd server && go test ./...)
  else
    printf '提示：未安装 Go，已跳过服务器测试。\n'
  fi
}

build_frontend() {
  heading '构建前端资源'
  "$VITE_BIN" build
}

package_macos() {
  [[ "$(uname -s)" == 'Darwin' ]] || fail 'macOS 包只能在 macOS 上生成'
  require_command hdiutil
  require_command ditto

  local machine_arch tauri_arch output_arch
  machine_arch="$(uname -m)"
  case "$machine_arch" in
    arm64)
      tauri_arch='aarch64'
      output_arch='arm64'
      ;;
    x86_64)
      tauri_arch='x64'
      output_arch='x64'
      ;;
    *)
      fail "不支持的 Mac 架构：$machine_arch"
      ;;
  esac

  heading "生成 macOS ${output_arch} 图标"
  PLATFORM=macos PATH="$ROOT_DIR/node_modules/.bin:$PATH" \
    "$TSX_BIN" scripts/buildIcon.ts

  heading "生成 macOS ${output_arch} DMG"
  "$TAURI_BIN" build \
    --bundles dmg \
    --no-sign \
    --config '{"build":{"beforeBuildCommand":""},"bundle":{"createUpdaterArtifacts":false}}'

  heading '重新生成可直接运行的 BongoCat.app'
  "$TAURI_BIN" bundle \
    --bundles app \
    --no-sign \
    --config '{"bundle":{"createUpdaterArtifacts":false}}'

  local app_source dmg_source dmg_name
  app_source="$ROOT_DIR/target/release/bundle/macos/BongoCat.app"
  dmg_source="$ROOT_DIR/target/release/bundle/dmg/BongoCat_${VERSION}_${tauri_arch}.dmg"
  dmg_name="BongoCat_${VERSION}_macos_${output_arch}.dmg"
  [[ -d "$app_source" ]] || fail "未找到应用包：$app_source"
  [[ -f "$dmg_source" ]] || fail "未找到磁盘镜像：$dmg_source"

  hdiutil verify "$dmg_source"
  file "$app_source/Contents/MacOS/bongo-cat" | grep -q "$machine_arch" \
    || fail 'macOS 主程序架构校验失败'

  ditto "$app_source" "$OUTPUT_DIR/BongoCat.app"
  cp "$dmg_source" "$OUTPUT_DIR/$dmg_name"
  ARTIFACTS+=("$dmg_name")
}

prepare_windows_toolchain() {
  [[ "$(uname -s)" == 'Darwin' ]] || fail '此脚本的 Windows 交叉打包目前只支持 macOS 主机'
  require_command rustup '可执行：brew install rustup'

  local cargo_home rustup_home xwin_cache cargo_xwin toolchain_cargo toolchain_bin llvm_bin build_path
  cargo_home="$WINDOWS_TOOLCHAIN_DIR/cargo"
  rustup_home="$WINDOWS_TOOLCHAIN_DIR/rustup"
  xwin_cache="$WINDOWS_TOOLCHAIN_DIR/xwin"
  cargo_xwin="$cargo_home/bin/cargo-xwin"
  mkdir -p "$cargo_home" "$rustup_home" "$xwin_cache"

  heading '准备独立的 Windows Rust 工具链'
  if ! CARGO_HOME="$cargo_home" RUSTUP_HOME="$rustup_home" \
    rustup toolchain list | grep -q '^stable'; then
    CARGO_HOME="$cargo_home" RUSTUP_HOME="$rustup_home" \
      rustup toolchain install stable --profile minimal --target "$WINDOWS_TARGET"
  elif ! CARGO_HOME="$cargo_home" RUSTUP_HOME="$rustup_home" \
    rustup target list --installed --toolchain stable | grep -qx "$WINDOWS_TARGET"; then
    CARGO_HOME="$cargo_home" RUSTUP_HOME="$rustup_home" \
      rustup target add "$WINDOWS_TARGET" --toolchain stable
  fi

  toolchain_cargo="$(CARGO_HOME="$cargo_home" RUSTUP_HOME="$rustup_home" rustup which --toolchain stable cargo)"
  toolchain_bin="$(dirname "$toolchain_cargo")"

  if [[ ! -x "$cargo_xwin" ]]; then
    heading '首次安装 cargo-xwin'
    CARGO_HOME="$cargo_home" RUSTUP_HOME="$rustup_home" \
      PATH="$toolchain_bin:$PATH" \
      "$toolchain_cargo" install cargo-xwin --version 0.23.1 --locked
  fi

  local llvm_prefix
  llvm_bin=''
  if command -v brew >/dev/null 2>&1; then
    llvm_prefix="$(brew --prefix llvm 2>/dev/null || true)"
    if [[ -n "$llvm_prefix" ]]; then
      llvm_bin="$llvm_prefix/bin"
    fi
  fi
  build_path="$toolchain_bin:$cargo_home/bin"
  if [[ -n "$llvm_bin" && -d "$llvm_bin" ]]; then
    build_path="$build_path:$llvm_bin"
  fi
  build_path="$build_path:$PATH"

  for command_name in clang-cl lld-link llvm-lib llvm-rc makensis; do
    PATH="$build_path" command -v "$command_name" >/dev/null 2>&1 \
      || fail "缺少 $command_name。请执行：brew install llvm nsis"
  done

  WINDOWS_CARGO_HOME="$cargo_home"
  WINDOWS_RUSTUP_HOME="$rustup_home"
  WINDOWS_XWIN_CACHE="$xwin_cache"
  WINDOWS_CARGO_XWIN="$cargo_xwin"
  WINDOWS_BUILD_PATH="$build_path"
}

package_windows() {
  prepare_windows_toolchain

  heading '生成 Windows 图标'
  PLATFORM=windows PATH="$ROOT_DIR/node_modules/.bin:$PATH" \
    "$TSX_BIN" scripts/buildIcon.ts

  heading '生成 Windows x64 NSIS 安装包'
  CARGO_HOME="$WINDOWS_CARGO_HOME" \
    RUSTUP_HOME="$WINDOWS_RUSTUP_HOME" \
    XWIN_CACHE_DIR="$WINDOWS_XWIN_CACHE" \
    PATH="$WINDOWS_BUILD_PATH" \
    "$TAURI_BIN" build \
      --runner "$WINDOWS_CARGO_XWIN" \
      --target "$WINDOWS_TARGET" \
      --no-sign \
      --config '{"build":{"beforeBuildCommand":""},"bundle":{"targets":["nsis"],"createUpdaterArtifacts":false}}'

  local exe_source binary_source exe_name
  exe_source="$ROOT_DIR/target/$WINDOWS_TARGET/release/bundle/nsis/BongoCat_${VERSION}_x64-setup.exe"
  binary_source="$ROOT_DIR/target/$WINDOWS_TARGET/release/bongo-cat.exe"
  exe_name="BongoCat_${VERSION}_windows_x64-setup.exe"
  [[ -f "$exe_source" ]] || fail "未找到 Windows 安装包：$exe_source"
  [[ -f "$binary_source" ]] || fail "未找到 Windows 主程序：$binary_source"

  file "$binary_source" | grep -q 'x86-64' || fail 'Windows 主程序架构校验失败'
  file "$exe_source" | grep -q 'Nullsoft Installer' || fail 'NSIS 安装包格式校验失败'

  cp "$exe_source" "$OUTPUT_DIR/$exe_name"
  ARTIFACTS+=("$exe_name")
}

write_checksums() {
  heading '生成 SHA-256 校验文件'
  (
    cd "$OUTPUT_DIR"
    shasum -a 256 "${ARTIFACTS[@]}" | tee SHA256SUMS.txt
  )
}

main() {
  parse_args "$@"
  cd "$ROOT_DIR"

  require_command node
  require_command cargo '请先安装 Rust'
  require_command file
  require_command shasum
  for local_binary in "$TAURI_BIN" "$VITE_BIN" "$VITEST_BIN" "$ESLINT_BIN" "$TSX_BIN"; do
    [[ -x "$local_binary" ]] || fail '缺少前端依赖，请先执行 pnpm install'
  done

  mkdir -p "$OUTPUT_DIR"
  run_checks
  build_frontend

  case "$TARGET" in
    macos)
      package_macos
      ;;
    windows)
      package_windows
      ;;
    all)
      package_macos
      package_windows
      ;;
  esac

  write_checksums
  printf '\n打包完成：%s\n' "$OUTPUT_DIR"
  printf '注意：本地包未签名，macOS Gatekeeper 或 Windows SmartScreen 可能显示提示。\n'
}

main "$@"
