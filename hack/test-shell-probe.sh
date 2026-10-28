#!/usr/bin/env bash
# Copyright (c) 2026 Benjamin Borbe All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
#
# Exercises images/shell-probe/probe.sh on the host — no Docker daemon needed.
#
# It asserts the extraction contract the entrypoint documents, because the
# failure that matters is silent: a regression in the opening/closing-fence
# regex would still produce a green build and a Job that runs the wrong thing
# (or nothing). The unclosed-fence case is the dangerous one — it would execute
# the rest of the task body as shell.
#
# Run via `make test` (target `test-shell-probe`).
set -uo pipefail

SCRIPT="$(cd "$(dirname "$0")/.." && pwd)/images/shell-probe/probe.sh"
[ -x "$SCRIPT" ] || { echo "FAIL: $SCRIPT is missing or not executable" >&2; exit 1; }

fail=0
pass=0

# run_case <name> <expected-exit> <expected-stdout-substring-or-empty> <task-content>
run_case() {
	local name="$1" want_exit="$2" want_out="$3" content="$4"
	local out rc
	out=$(TASK_CONTENT="$content" "$SCRIPT" 2>&1)
	rc=$?
	if [ "$rc" -ne "$want_exit" ]; then
		echo "FAIL [$name]: exit $rc, want $want_exit" >&2
		printf '%s\n' "$out" >&2
		fail=$((fail + 1))
		return
	fi
	if [ -n "$want_out" ] && ! printf '%s' "$out" | grep -qF -- "$want_out"; then
		echo "FAIL [$name]: output missing '$want_out'" >&2
		printf '%s\n' "$out" >&2
		fail=$((fail + 1))
		return
	fi
	pass=$((pass + 1))
}

# 1. a valid fence runs, and the block's output appears
run_case "valid fence" 0 "probe-ok" \
	$'---\ntitle: t\n---\n\n```probe-sh\necho probe-ok\n```\n'

# 2. prose outside the fence is NEVER executed
run_case "prose not executed" 0 "" \
	$'echo PROSE_LEAKED\n\n```probe-sh\necho in-block\n```\n\necho PROSE2_LEAKED\n'
out=$(TASK_CONTENT=$'echo PROSE_LEAKED\n\n```probe-sh\necho in-block\n```\n' "$SCRIPT" 2>&1)
if printf '%s' "$out" | grep -q 'PROSE_LEAKED'; then
	echo "FAIL [prose not executed]: prose was executed" >&2
	fail=$((fail + 1))
else
	pass=$((pass + 1))
fi

# 3. no fence at all refuses
run_case "no fence" 2 "no 'probe-sh' fenced block" $'# Task\n\njust prose\n'

# 4. an UNCLOSED fence refuses rather than running to EOF
run_case "unclosed fence" 2 "unterminated" \
	$'```probe-sh\necho should-not-run\n'

# 5. an empty block refuses
run_case "empty block" 2 "empty" $'```probe-sh\n```\n'

# 6. a failing command inside the block still yields exit 0
run_case "failing command exits 0" 0 "run end rc=" \
	$'```probe-sh\nfalse\necho after-false\n```\n'

# 7. CRLF line endings still match
run_case "crlf" 0 "probe-ok" \
	$'```probe-sh\r\necho probe-ok\r\n```\r\n'

# 8. an indented fence still matches
run_case "indented fence" 0 "probe-ok" \
	$'- a list item\n\n  ```probe-sh\n  echo probe-ok\n  ```\n'

# 9. PROBE_COMMAND overrides the fence and needs no task content
out=$(PROBE_COMMAND='echo from-env' TASK_CONTENT='' "$SCRIPT" 2>&1)
rc=$?
if [ "$rc" -eq 0 ] && printf '%s' "$out" | grep -qF 'from-env'; then
	pass=$((pass + 1))
else
	echo "FAIL [PROBE_COMMAND]: exit $rc, output:" >&2
	printf '%s\n' "$out" >&2
	fail=$((fail + 1))
fi

# 10. only the FIRST block runs
out=$(TASK_CONTENT=$'```probe-sh\necho first\n```\n\n```probe-sh\necho second\n```\n' "$SCRIPT" 2>&1)
if printf '%s' "$out" | grep -qF 'first' && ! printf '%s' "$out" | grep -qF 'second'; then
	pass=$((pass + 1))
else
	echo "FAIL [first block wins]: output:" >&2
	printf '%s\n' "$out" >&2
	fail=$((fail + 1))
fi

if [ "$fail" -ne 0 ]; then
	echo "FAIL: shell-probe tests — ${fail} failed, ${pass} passed" >&2
	exit 1
fi
echo "OK: shell-probe tests passed (${pass} cases)"
