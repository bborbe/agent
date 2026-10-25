{{/* Whether the sandbox egress layer renders: every NetworkPolicy the chart
     owns for the sandbox (the per-domain `<name>-egress`, the legacy
     `agent-sandbox-egress`, `egress-proxy-egress`, `egress-proxy-ingress`),
     every egress-proxy object (Deployment, Service, PodDisruptionBudget,
     allowlist ConfigMap), and the proxy env injected into Job agent Configs.
     Returns "true" or the empty string.

     `sandbox.renderPolicies` UNSET (or null) means true, so an install that
     never sets the key renders exactly what it rendered before the key
     existed. Only an explicit `false` turns the layer off; any other value
     fails the render rather than guessing.

     The sandbox Namespaces, the executor's sandbox RBAC and the per-agent
     Secret/PVC copies (`sandbox.renderAgentStorage`) do NOT read this flag:
     they keep rendering into the same namespaces, which is the point — an
     install can keep its agents placed while the egress layer is parked.
     The proxy env follows the proxy: injecting HTTP(S)_PROXY that names a
     Service this same render omits would send every agent's egress to a
     host that does not resolve. Input: the root context. */}}
{{- define "agent.sandboxRenderPolicies" -}}
{{- $sandbox := .Values.sandbox | default dict -}}
{{- $value := toString (index $sandbox "renderPolicies") -}}
{{- if or (eq $value "<nil>") (eq $value "true") -}}
true
{{- else if ne $value "false" -}}
{{- fail (printf "sandbox.renderPolicies must be true or false, got %q" $value) -}}
{{- end -}}
{{- end -}}
