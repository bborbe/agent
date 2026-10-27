# A shell-entrypoint probe image for sandboxed egress assertions.
#
# The agent platform has no way to give a Job a shell: `Config.spec` has no
# command/args field, and the executor's spawner (pkg/spawner/job_spawner.go) sets
# only Image, Env and resources on the container. Every existing agent image is
# either an LLM whose guardrail refuses private IPs and `*.svc` cluster DNS, or a
# Go binary with a fixed workload. This image is the third thing: a plain shell
# that runs a command the task body supplies.
#
# Deliberately tiny. busybox supplies `sh`, `nc` and `awk`; `curl` is the only
# package added. Every binary here is surface, and a probe needs exactly these
# three.
FROM alpine:3.23

RUN apk add --no-cache curl

COPY probe.sh /probe.sh
RUN chmod 0755 /probe.sh && adduser -D -u 65532 probe

# Non-root. The pod template already drops ALL capabilities and sets seccomp
# RuntimeDefault; this adds the one thing the template cannot express.
USER 65532

ENTRYPOINT ["/probe.sh"]
