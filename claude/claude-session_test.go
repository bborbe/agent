// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package claude_test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/claude"
	"github.com/bborbe/agent/mocks"
)

// sessionShimBase is a long-lived fake CLI: it stays alive across turns, records
// every byte written to its stdin, and answers each turn with a numbered result.
// It reads $SHIM_DIR rather than dirname "$0" because the process is spawned by
// name and never learns its own path.
const sessionShimBase = `trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
echo "$$" >> "$SHIM_DIR/starts.log"
printf '%s\n' "$@" > "$SHIM_DIR/args.log"
n=0
while IFS= read -r line; do
  n=$((n+1))
  printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
  printf '%s\n' "$$" > "$SHIM_DIR/turn-$n.marker"
  printf '{"type":"result","result":"reply-%s","session_id":"sess-fixed","num_turns":1}\n' "$n"
done
exit 0
`

// sessionShimPermission raises a permission request mid-turn and blocks until the
// verdict arrives on stdin, then finishes the turn. The tool input is deliberately
// longer than the preview cap so the truncation is observable.
var sessionShimPermission = fmt.Sprintf(
	`trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
read -r line
printf '%%s\n' "$line" >> "$SHIM_DIR/stdin.log"
printf '{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"%s"}}}\n'
read -r resp
printf '%%s\n' "$resp" >> "$SHIM_DIR/stdin.log"
printf '{"type":"result","result":"done-after-permission"}\n'
`,
	strings.Repeat("x", 600),
)

// sessionShimPermissionShort raises a permission request with a tool input that
// fits inside the preview cap, so the un-truncated path is exercised too.
const sessionShimPermissionShort = `trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
read -r line
printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
printf '{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}\n'
read -r resp
printf '%s\n' "$resp" >> "$SHIM_DIR/stdin.log"
printf '{"type":"result","result":"done-after-permission"}\n'
`

// sessionShimPermissionClosedStdin raises a permission request and then closes its
// own stdin, so the verdict cannot be written back.
const sessionShimPermissionClosedStdin = `trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
read -r line
exec 0<&-
printf '{"type":"control_request","request_id":"req-1","request":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"ls"}}}\n'
sleep 30 >/dev/null 2>&1 &
wait
`

// sessionShimCancellable records the turn and then blocks in a way a signal can
// interrupt. The background sleep redirects its stdout so it does not hold the
// process's stdout pipe open after the shell itself has exited.
const sessionShimCancellable = `trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
trap 'exit 1' TERM INT
echo "$$" >> "$SHIM_DIR/starts.log"
read -r line
printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
sleep 30 >/dev/null 2>&1 &
wait
printf '{"type":"result","result":"too-late"}\n'
`

// captureGlog redirects the process's stderr into a buffer for the duration of fn
// and returns what was written. glog's stderr sink reads os.Stderr when it emits,
// so the swap is observed as long as -logtostderr is on.
func captureGlog(fn func()) string {
	old := os.Stderr
	r, w, err := os.Pipe()
	Expect(err).To(BeNil())
	os.Stderr = w
	defer func() { os.Stderr = old }()

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	Expect(w.Close()).To(Succeed())
	os.Stderr = old
	return <-done
}

var _ = Describe("claudeSession held process", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	// writeSessionShim creates a temp dir, writes a "claude" shell script with the
	// given body into it, prepends the dir to PATH and returns the dir so the test
	// can hand it to the session through ClaudeRunnerConfig.Env.
	writeSessionShim := func(body string) string {
		shimDir := GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "claude")
		script := "#!/bin/sh\n" + body
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
		return shimDir
	}

	// sessionConfig points the session at the shim through the existing
	// consumer-provided Env escape hatch — no new config field is introduced.
	sessionConfig := func(shimDir string) claude.ClaudeRunnerConfig {
		return claude.ClaudeRunnerConfig{Env: map[string]string{"SHIM_DIR": shimDir}}
	}

	openSession := func(shimDir string, decider claude.PermissionDecider) agentlib.Session {
		session := claude.NewSession(sessionConfig(shimDir), decider)
		DeferCleanup(func() { _ = session.Close(context.Background()) })
		return session
	}

	readLines := func(path string) []string {
		raw, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			return nil
		}
		return strings.Split(trimmed, "\n")
	}

	It("starts exactly one process across two sends", func() {
		shimDir := writeSessionShim(sessionShimBase)
		session := openSession(shimDir, nil)

		_, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Prompt(ctx, "turn-two")
		Expect(err).NotTo(HaveOccurred())

		Expect(readLines(filepath.Join(shimDir, "starts.log"))).To(HaveLen(1))
	})

	It("writes both turns' payloads to that one process's stdin", func() {
		shimDir := writeSessionShim(sessionShimBase)
		session := openSession(shimDir, nil)

		first, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())
		second, err := session.Prompt(ctx, "turn-two")
		Expect(err).NotTo(HaveOccurred())

		Expect(first).To(Equal("reply-1"))
		Expect(second).To(Equal("reply-2"))

		lines := readLines(filepath.Join(shimDir, "stdin.log"))
		Expect(lines).To(HaveLen(2))
		Expect(lines[0]).To(ContainSubstring("turn-one"))
		Expect(lines[1]).To(ContainSubstring("turn-two"))

		var turn struct {
			Type    string `json:"type"`
			Message struct {
				Role    string `json:"role"`
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		Expect(json.Unmarshal([]byte(lines[0]), &turn)).To(Succeed())
		Expect(turn.Type).To(Equal("user"))
		Expect(turn.Message.Role).To(Equal("user"))
		Expect(turn.Message.Content).To(HaveLen(1))
		Expect(turn.Message.Content[0].Text).To(Equal("turn-one"))
	})

	It("the process is still running when the second turn is written", func() {
		shimDir := writeSessionShim(sessionShimBase)
		session := openSession(shimDir, nil)

		_, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())
		_, err = session.Prompt(ctx, "turn-two")
		Expect(err).NotTo(HaveOccurred())

		markerPath := filepath.Join(shimDir, "turn-2.marker")
		Expect(markerPath).To(BeAnExistingFile())
		rawMarker, err := os.ReadFile(markerPath)
		Expect(err).NotTo(HaveOccurred())
		pidText := strings.TrimSpace(string(rawMarker))

		// The pid that consumed turn 2 is the pid that started — a *exec.Cmd identity
		// check would survive process death and prove neither.
		Expect(readLines(filepath.Join(shimDir, "starts.log"))).To(Equal([]string{pidText}))

		pid, err := strconv.Atoi(pidText)
		Expect(err).NotTo(HaveOccurred())
		proc, err := os.FindProcess(pid)
		Expect(err).NotTo(HaveOccurred())
		Expect(proc.Signal(syscall.Signal(0))).To(Succeed())
	})

	It("spawns with the stream-json protocol flags", func() {
		shimDir := writeSessionShim(sessionShimBase)
		session := openSession(shimDir, nil)

		_, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())

		args := readLines(filepath.Join(shimDir, "args.log"))
		Expect(args).To(ContainElement("--print"))
		Expect(args).To(ContainElement("--input-format"))
		Expect(args).To(ContainElement("--output-format"))
		Expect(args).To(ContainElement("stream-json"))
		Expect(args).To(ContainElement("--verbose"))

		inputIdx := indexOf(args, "--input-format")
		outputIdx := indexOf(args, "--output-format")
		Expect(inputIdx).To(BeNumerically(">=", 0))
		Expect(outputIdx).To(BeNumerically(">=", 0))
		Expect(args[inputIdx+1]).To(Equal("stream-json"))
		Expect(args[outputIdx+1]).To(Equal("stream-json"))
	})

	It("creates the process in exactly one place", func() {
		source, err := os.ReadFile("claude-session.go")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Count(string(source), "exec.Command(")).To(Equal(1))
	})
})

var _ = Describe("claudeSession failure and permission", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	writeSessionShim := func(body string) string {
		shimDir := GinkgoT().TempDir()
		shimPath := filepath.Join(shimDir, "claude")
		script := "#!/bin/sh\n" + body
		Expect(os.WriteFile(shimPath, []byte(script), 0755)).To(Succeed()) //nolint:gosec
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", shimDir+":"+originalPath)).To(Succeed())
		return shimDir
	}

	sessionConfig := func(shimDir string) claude.ClaudeRunnerConfig {
		return claude.ClaudeRunnerConfig{Env: map[string]string{"SHIM_DIR": shimDir}}
	}

	readLines := func(path string) []string {
		raw, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		trimmed := strings.TrimSpace(string(raw))
		if trimmed == "" {
			return nil
		}
		return strings.Split(trimmed, "\n")
	}

	It("fails the next turn when the held process died between turns", func() {
		shimDir := writeSessionShim(`trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
echo "$$" >> "$SHIM_DIR/starts.log"
read -r line
printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
printf '{"type":"result","result":"reply-1"}\n'
exit 0
`)
		session := claude.NewSession(sessionConfig(shimDir), nil)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		first, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())
		Expect(first).To(Equal("reply-1"))

		_, err = session.Prompt(ctx, "turn-two")
		Expect(err).To(HaveOccurred())

		// No re-spawn: a silent fresh conversation would show a second start line.
		Expect(readLines(filepath.Join(shimDir, "starts.log"))).To(HaveLen(1))
	})

	It("fails the turn on an unparseable event without logging its content", func() {
		malformed := `{"type":"assistant","text":"LEAK-CANARY not valid json`
		shimDir := writeSessionShim(fmt.Sprintf(
			`read -r line
printf '%%s\n' '%s'
sleep 30 >/dev/null 2>&1 &
wait
`, malformed,
		))
		session := claude.NewSession(sessionConfig(shimDir), nil)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		Expect(flag.Set("logtostderr", "true")).To(Succeed())
		Expect(flag.Set("v", "4")).To(Succeed())

		var result string
		var err error
		out := captureGlog(func() {
			result, err = session.Prompt(ctx, "turn-one")
		})

		Expect(err).To(HaveOccurred())
		Expect(result).To(BeEmpty())
		Expect(err.Error()).NotTo(ContainSubstring("LEAK-CANARY"))
		Expect(err.Error()).To(ContainSubstring(
			fmt.Sprintf("%d bytes", len(malformed)),
		))
		Expect(out).NotTo(ContainSubstring("LEAK-CANARY"))
	})

	It("routes a permission request out and writes the verdict back", func() {
		shimDir := writeSessionShim(sessionShimPermission)
		decider := &mocks.ClaudePermissionDecider{}
		decider.DecidePermissionReturns(claude.PermissionDecision{Allow: true}, nil)
		session := claude.NewSession(sessionConfig(shimDir), decider)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		result, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("done-after-permission"))

		Expect(decider.DecidePermissionCallCount()).To(Equal(1))
		_, request := decider.DecidePermissionArgsForCall(0)
		Expect(request.ToolName).To(Equal("Bash"))
		Expect(request.InputPreview).To(HaveLen(512))

		lines := readLines(filepath.Join(shimDir, "stdin.log"))
		Expect(lines).To(HaveLen(2))

		var response struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Response  struct {
				Response struct {
					Behavior string `json:"behavior"`
				} `json:"response"`
			} `json:"response"`
		}
		Expect(json.Unmarshal([]byte(lines[1]), &response)).To(Succeed())
		Expect(response.RequestID).To(Equal("req-1"))
		Expect(response.Response.Response.Behavior).To(Equal("allow"))
	})

	It("fails the turn when the permission decider errors", func() {
		shimDir := writeSessionShim(sessionShimPermission)
		decider := &mocks.ClaudePermissionDecider{}
		decider.DecidePermissionReturns(claude.PermissionDecision{}, fmt.Errorf("no surface"))
		session := claude.NewSession(sessionConfig(shimDir), decider)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		result, err := session.Prompt(ctx, "turn-one")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no surface"))
		Expect(result).To(BeEmpty())
	})

	It("builds the same argv regardless of the session id", func() {
		shimDir := writeSessionShim(sessionShimBase)
		factory := claude.NewSessionFactory(sessionConfig(shimDir), nil)

		first := factory.Create("id-one")
		DeferCleanup(func() { _ = first.Close(context.Background()) })
		_, err := first.Prompt(ctx, "hello")
		Expect(err).NotTo(HaveOccurred())
		firstArgs, err := os.ReadFile(filepath.Join(shimDir, "args.log"))
		Expect(err).NotTo(HaveOccurred())

		second := factory.Create("id-two")
		DeferCleanup(func() { _ = second.Close(context.Background()) })
		_, err = second.Prompt(ctx, "hello")
		Expect(err).NotTo(HaveOccurred())
		secondArgs, err := os.ReadFile(filepath.Join(shimDir, "args.log"))
		Expect(err).NotTo(HaveOccurred())

		Expect(string(secondArgs)).To(Equal(string(firstArgs)))
	})

	It("terminates the process on Close", func() {
		shimDir := writeSessionShim(sessionShimBase)
		session := claude.NewSession(sessionConfig(shimDir), nil)

		_, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())

		Expect(session.Close(ctx)).To(Succeed())

		exited := filepath.Join(shimDir, "exited.log")
		Eventually(exited, 5*time.Second).Should(BeAnExistingFile())

		_, err = session.Prompt(ctx, "turn-two")
		Expect(err).To(HaveOccurred())
	})

	It("terminates the process when an in-flight turn's context is cancelled", func() {
		shimDir := writeSessionShim(sessionShimCancellable)
		session := claude.NewSession(sessionConfig(shimDir), nil)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		cancelCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, err := session.Prompt(cancelCtx, "turn-one")
		Expect(err).To(HaveOccurred())

		_, err = session.Prompt(context.Background(), "turn-two")
		Expect(err).To(HaveOccurred())
		Expect(readLines(filepath.Join(shimDir, "starts.log"))).To(HaveLen(1))

		// The session must actually have terminated the process, not merely returned
		// the context's error while the shim kept running.
		exited := filepath.Join(shimDir, "exited.log")
		Eventually(exited, 5*time.Second).Should(BeAnExistingFile())

		pidText := strings.TrimSpace(string(readFile(exited)))
		pid, convErr := strconv.Atoi(pidText)
		Expect(convErr).NotTo(HaveOccurred())
		Eventually(func() error {
			proc, findErr := os.FindProcess(pid)
			if findErr != nil {
				return findErr
			}
			return proc.Signal(syscall.Signal(0))
		}, 5*time.Second).Should(HaveOccurred())
	})

	It("fails the turn when the process exits without a result event", func() {
		shimDir := writeSessionShim(`trap 'echo "$$" > "$SHIM_DIR/exited.log"' EXIT
read -r line
printf '%s\n' "$line" >> "$SHIM_DIR/stdin.log"
exit 0
`)
		session := claude.NewSession(sessionConfig(shimDir), nil)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		result, err := session.Prompt(ctx, "turn-one")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ended mid-turn"))
		Expect(result).To(BeEmpty())
	})

	It("fails the turn when the process cannot be started", func() {
		emptyPath := GinkgoT().TempDir()
		originalPath := os.Getenv("PATH")
		DeferCleanup(func() {
			Expect(os.Setenv("PATH", originalPath)).To(Succeed())
		})
		Expect(os.Setenv("PATH", emptyPath)).To(Succeed())

		session := claude.NewSession(claude.ClaudeRunnerConfig{}, nil)

		_, err := session.Prompt(ctx, "turn-one")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("start claude process"))
	})

	It("runs the process in the configured working directory", func() {
		workDir, err := filepath.EvalSymlinks(GinkgoT().TempDir())
		Expect(err).NotTo(HaveOccurred())
		shimDir := writeSessionShim(`read -r line
printf '{"type":"result","result":"PWD=%s"}\n' "$PWD"
`)
		session := claude.NewSession(claude.ClaudeRunnerConfig{
			WorkingDirectory: claude.AgentDir(workDir),
			Env:              map[string]string{"SHIM_DIR": shimDir},
		}, nil)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		result, err := session.Prompt(ctx, "turn-one")

		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(Equal("PWD=" + workDir))
	})

	It("passes the configured tools and model to the process", func() {
		shimDir := writeSessionShim(sessionShimBase)
		session := claude.NewSession(claude.ClaudeRunnerConfig{
			AllowedTools: claude.ParseAllowedTools("Read,Write"),
			Model:        claude.SonnetClaudeModel,
			Env:          map[string]string{"SHIM_DIR": shimDir},
		}, nil)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		_, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())

		args := readLines(filepath.Join(shimDir, "args.log"))
		toolsIdx := indexOf(args, "--allowedTools")
		modelIdx := indexOf(args, "--model")
		Expect(toolsIdx).To(BeNumerically(">=", 0))
		Expect(modelIdx).To(BeNumerically(">=", 0))
		Expect(args[toolsIdx+1]).To(Equal("Read,Write"))
		Expect(args[modelIdx+1]).To(Equal("sonnet"))
	})

	It("writes a denial back when the decider denies", func() {
		shimDir := writeSessionShim(sessionShimPermissionShort)
		decider := &mocks.ClaudePermissionDecider{}
		decider.DecidePermissionReturns(
			claude.PermissionDecision{Allow: false, Message: "not allowed"},
			nil,
		)
		session := claude.NewSession(sessionConfig(shimDir), decider)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		_, err := session.Prompt(ctx, "turn-one")
		Expect(err).NotTo(HaveOccurred())

		_, request := decider.DecidePermissionArgsForCall(0)
		Expect(request.InputPreview).To(Equal(`{"command":"ls"}`))

		var response struct {
			Response struct {
				Response struct {
					Behavior string `json:"behavior"`
					Message  string `json:"message"`
				} `json:"response"`
			} `json:"response"`
		}
		Expect(json.Unmarshal(
			[]byte(readLines(filepath.Join(shimDir, "stdin.log"))[1]),
			&response,
		)).To(Succeed())
		Expect(response.Response.Response.Behavior).To(Equal("deny"))
		Expect(response.Response.Response.Message).To(Equal("not allowed"))
	})

	It("fails the turn on an unsupported control request", func() {
		bodies := []string{
			`read -r line
printf '{"type":"control_request","request_id":"req-9"}\n'
`,
			`read -r line
printf '{"type":"control_request","request_id":"req-9","request":{"subtype":"set_permission_mode"}}\n'
`,
		}
		for _, body := range bodies {
			shimDir := writeSessionShim(body)
			session := claude.NewSession(sessionConfig(shimDir), &mocks.ClaudePermissionDecider{})

			_, err := session.Prompt(ctx, "turn-one")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unsupported control request"))
			_ = session.Close(context.Background())
		}
	})

	It("fails the turn when the verdict cannot be written back", func() {
		shimDir := writeSessionShim(sessionShimPermissionClosedStdin)
		decider := &mocks.ClaudePermissionDecider{}
		decider.DecidePermissionReturns(claude.PermissionDecision{Allow: true}, nil)
		session := claude.NewSession(sessionConfig(shimDir), decider)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		_, err := session.Prompt(ctx, "turn-one")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("write permission response to stdin"))
	})

	It("fails the turn when a permission is requested with no decider", func() {
		shimDir := writeSessionShim(sessionShimPermission)
		session := claude.NewSession(sessionConfig(shimDir), nil)
		DeferCleanup(func() { _ = session.Close(context.Background()) })

		_, err := session.Prompt(ctx, "turn-one")

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no decider is configured"))
	})
})

// indexOf returns the position of value in values, or -1.
func indexOf(values []string, value string) int {
	for i, candidate := range values {
		if candidate == value {
			return i
		}
	}
	return -1
}

// readFile reads a file that a test has already established exists.
func readFile(path string) []byte {
	raw, err := os.ReadFile(path)
	Expect(err).NotTo(HaveOccurred())
	return raw
}
