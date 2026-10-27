#!/bin/sh
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
# ONLY that fence is executed. Prose in the task body is never run, and a task
# with no such fence exits 2 without executing anything.
#
# PROBE_COMMAND overrides the fence. The probe Config does NOT set it — it exists
# so this image can be exercised with `kubectl run` before any Config exists, and
# so a chart-pinned command has a path that never touches the task body.
#
# The exit status is ALWAYS 0 once a block was found. A refused connect is the
# *result* this vehicle exists to record; a non-zero exit would make the executor
# publish a synthetic failure, mark the task failed, and hide the denial.
set -u

block=""
if [ -n "${PROBE_COMMAND:-}" ]; then
	echo "probe: command source=PROBE_COMMAND"
	block="$PROBE_COMMAND"
else
	echo "probe: command source=TASK_CONTENT fence"
	block=$(printf '%s\n' "${TASK_CONTENT:-}" | awk '
		/^```probe-sh[ \t]*$/ { inblock = 1; next }
		inblock && /^```[ \t]*$/ { exit }
		inblock { print }
	')
fi

if [ -z "$block" ]; then
	echo "probe: FATAL no 'probe-sh' fenced block in TASK_CONTENT and no PROBE_COMMAND — nothing executed" >&2
	exit 2
fi

echo "probe: task=${TASK_ID:-unknown} host=$(hostname)"

echo "probe: ---- block begin ----"
printf '%s\n' "$block"
echo "probe: ---- block end ----"

printf '%s\n' "$block" > /tmp/probe-block.sh

# `sh -x` so the log carries each command verbatim next to its output — that is
# what makes the log quotable as "the command and its result from inside the pod".
echo "probe: ---- run begin (sh -x) ----"
sh -x /tmp/probe-block.sh 2>&1
rc=$?
echo "probe: ---- run end rc=${rc} ----"

exit 0
