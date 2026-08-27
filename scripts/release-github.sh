#!/usr/bin/env bash

set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DRY_RUN=0

usage() {
  cat <<'EOF'
用法：
  ./scripts/release-github.sh [--dry-run]
  pnpm release:github [--dry-run]

脚本会读取 package.json 的版本，推送 main 分支和对应的 v<version> 标签。
标签会触发 GitHub Actions，并行构建以下未商业签名的安装包：
  - macOS：ARM64、x86_64（DMG）
  - Windows：ARM64、x86_64（NSIS EXE）
  - Linux：ARM64、x86_64（DEB、RPM）

--dry-run 仅校验本地状态，不创建或推送标签。
EOF
}

fail() {
  printf '错误：%s\n' "$1" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "缺少命令 '$1'"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
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

cd "$ROOT_DIR"
require_command node
require_command git
require_command gh

VERSION="$(node -p "require('./package.json').version")"
TAG="v${VERSION}"
BRANCH="$(git branch --show-current)"

[[ "$BRANCH" == 'main' ]] || fail "只能从 main 分支发布，当前分支为 ${BRANCH:-未知}"
[[ -z "$(git status --porcelain)" ]] || fail '工作区存在未提交修改或未跟踪文件'
gh auth status --hostname github.com >/dev/null

git fetch origin --tags

if git rev-parse --verify --quiet "refs/tags/$TAG" >/dev/null; then
  fail "标签 $TAG 已存在"
fi

if [[ "$DRY_RUN" -eq 1 ]]; then
  printf '校验通过：将从 main 创建并推送标签 %s\n' "$TAG"
  exit 0
fi

git push origin main
git tag -a "$TAG" -m "BongoCat $TAG"
git push origin "$TAG"

printf '已触发多平台发布：%s/actions/workflows/release.yml\n' \
  "$(gh repo view --json url --jq '.url')"
