#!/bin/sh
set -eu

required_files="
AGENTS.md
.ai/manifest.yaml
.ai/template.yaml
.ai/rules/common.md
.ai/service.yaml
.ai/commands.yaml
.ai/agents/coder.md
.ai/agents/reviewer.md
.ai/workflows/bugfix.yaml
.ai/workflows/implement-feature.yaml
.ai/workflows/refactor.yaml
.ai/workflows/review.yaml
.ai/workflows/issue-delivery.yaml
docs/README.md
"

for path in $required_files; do
  if [ ! -f "$path" ]; then
    echo "agent-policy: missing $path" >&2
    exit 1
  fi
done

obsolete_paths='prom''pts/|prom''ps/|jour''nal/|microservices/wi''ki|/wi''ki/'
if grep -R -n -E "$obsolete_paths" AGENTS.md .ai docs README.md 2>/dev/null | grep -v -E '^\.ai/testing/policy/policy-runner\.cjs:[0-9]+:'; then
  echo "agent-policy: obsolete external knowledge reference found" >&2
  exit 1
fi

echo "agent-policy: ok"
