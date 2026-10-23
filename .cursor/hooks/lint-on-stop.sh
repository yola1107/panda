#!/bin/bash
# Agent 结束回合时检查 Go 改动：全仓 lint 必须零告警，手写代码不得出现 //nolint。
# 未改动 Go 相关文件时跳过；检查失败时把输出交回 Agent 继续修复。

input=$(cat)
status=$(printf '%s' "$input" | jq -r '.status // empty')
[ "$status" = "completed" ] || exit 0

export PATH="$(go env GOPATH)/bin:$PATH"

changed=$(git status --porcelain --untracked-files=all -- '*.go' go.mod go.sum .golangci.yml)
[ -n "$changed" ] || exit 0

nl=$'\n'
report=""
if ! lint_output=$(make lint 2>&1); then
	report="make lint 未通过：${nl}${lint_output}${nl}"
fi

nolint=""
while IFS= read -r file; do
	grep -q 'Code generated .* DO NOT EDIT' "$file" || nolint="${nolint}${file}${nl}"
done < <(git grep -l --untracked '//nolint' -- '*.go')
if [ -n "$nolint" ]; then
	report="${report}手写 Go 文件出现 //nolint：${nl}${nolint}"
fi

[ -n "$report" ] || exit 0

message="${report}按 .golangci.yml 修复代码，不要用 //nolint 或修改规则绕过；修复后重新运行 make lint 确认零告警。"
jq -n --arg msg "$(printf '%s' "$message" | tail -c 20000)" '{followup_message: $msg}'
