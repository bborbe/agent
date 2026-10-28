# A shell-entrypoint probe image for sandboxed egress assertions.
#
# The agent platform has no way to give a Job a shell: `Config.spec` has no
# command/args field, and the executor's spawner (pkg/spawner/job_spawner.go) sets
# only Image, Env and resources on the container. Every existing agent image is
# either an LLM whose guardrail refuses private IPs and `*.svc` cluster DNS, or a
# Go binary with a fixed workload. This image is the third thing: a plain shell
# that runs a command the task body supplies.
#
# Pinned by digest, not by tag. This image decides whether egress assertions are
# trustworthy, so a rebuild of the same VERSION must produce the same image —
# the repo already pins the analogous load-bearing egress self-test image
# (helm/templates/egress-proxy.yaml, "as load-bearing as the proxy itself").
# `alpine:3.23` index digest, read 2026-10-28.
FROM alpine:3.23@sha256:ff7f0c727dd796aa36787bfcd9a64bf35a6ce72a73451b3359fde849b6703d63

# `curl` is not in busybox, and busybox `wget` cannot express `curl -x` with
# `%{http_code}` — the proxy probe needs both. Everything else (sh, nc, awk) is
# busybox and is deliberately not replaced.
RUN apk add --no-cache curl

COPY probe.sh /probe.sh
RUN chmod 0755 /probe.sh && adduser -D -u 65532 probe

# Non-root. The pod template already drops ALL capabilities and sets seccomp
# RuntimeDefault; this adds the one thing the template cannot express.
USER 65532

ENTRYPOINT ["/probe.sh"]
