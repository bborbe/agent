#!/bin/sh
# Copyright (c) 2026 Benjamin Borbe All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.
#
# Runs ONE shell command block taken from the task body.
#
# The agent platform cannot give a Job a shell: `Config.spec` has no command/args
# field, and the executor's spawner sets only Image, Env and resources on the
# container — never Command/Args. So the shell has to come from the image, and the
# command from somewhere the image can read. This is that image's entrypoint.
#
# The command source is a fenced block in the task markdown whose info string is
# exactly `probe-sh`:
#
#     ```probe-sh
#     nc -w 3 192.168.178.41 32159 </dev/null; echo "kafka-41=$?"
#     ```
#
# ONLY that fence is executed. Prose in the task body is never run.
#
# Extraction contract (all cases covered by hack/test-shell-probe.sh):
#   - leading/trailing whitespace on the fence line is tolerated
#   - CRLF line endings are normalised
#   - the FIRST `probe-sh` block wins; later blocks are ignored
#   - an UNCLOSED fence is refused (exit 2) rather than executed to EOF — the
#     dangerous case, since it would run the rest of the task body as shell
#   - an empty block is refused (exit 2)
#   - no fence at all is refused (exit 2)
#   - a parser failure is reported as such, never as "no fence"
#
# PROBE_COMMAND overrides the fence. The probe Config does NOT set it — it exists
# so this image can be exercised with `kubectl run` before any Config exists, and
# so a chart-pinned command has a path that never touches the task body.
#
# The exit status is ALWAYS 0 once a non-empty block was found and executed. A
# refused connect is the *result* this vehicle exists to record; a non-zero exit
# would make the executor publish a synthetic failure, mark the task failed, and
# hide the denial. Every refusal above exits 2, which the executor treats as a
# crashed Job — the right signal for "the vehicle could not run the probe".
set -u

block=""
if [ -n "${PROBE_COMMAND:-}" ]; then
	echo "probe: command source=PROBE_COMMAND"
	block="$PROBE_COMMAND"
else
	echo "probe: command source=TASK_CONTENT fence"
	# `tr -d '\r'` normalises CRLF. awk's exit status distinguishes the failure
	# modes: 4 = no fence, 3 = fence never closed, anything else = parser error.
	block=$(printf '%s\n' "${TASK_CONTENT:-}" | tr -d '\r' | awk '
		BEGIN { found = 0; inblock = 0; closed = 0 }
		{
			line = $0
			if (!found && line ~ /^[ \t]*```probe-sh[ \t]*$/) {
				found = 1
				inblock = 1
				next
			}
			if (inblock && line ~ /^[ \t]*```[ \t]*$/) {
				inblock = 0
				closed = 1
				exit
			}
			if (inblock) print
		}
		END {
			if (!found) exit 4
			if (!closed) exit 3
			exit 0
		}
	')
	rc=$?
	case "$rc" in
	0) ;;
	3)
		echo "probe: FATAL unterminated 'probe-sh' fence — refusing to execute the rest of the task body" >&2
		exit 2
		;;
	4)
		echo "probe: FATAL no 'probe-sh' fenced block in TASK_CONTENT — nothing executed" >&2
		exit 2
		;;
	*)
		echo "probe: FATAL fence extraction failed (awk exit ${rc})" >&2
		exit 2
		;;
	esac
fi

if [ -z "$block" ]; then
	echo "probe: FATAL the 'probe-sh' block is empty — nothing to execute" >&2
	exit 2
fi

echo "probe: task=${TASK_ID:-unknown} host=$(hostname)"

# The block is deliberately NOT echoed. `sh -x` below records each command
# verbatim next to its output, which is what makes the log quotable; printing it
# as well would only double the exposure of anything a probe command carries.
printf '%s\n' "$block" > /tmp/probe-block.sh

echo "probe: ---- run begin (sh -x) ----"
sh -x /tmp/probe-block.sh 2>&1
rc=$?
echo "probe: ---- run end rc=${rc} ----"

exit 0
