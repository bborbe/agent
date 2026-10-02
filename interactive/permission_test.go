// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/claude"
	"github.com/bborbe/agent/interactive"
	"github.com/bborbe/agent/mocks"
)

// permissionTool is the tool name the fixture raises.
const permissionTool = "Bash"

// permissionPrompt is the prompt that makes a fixture session raise a request. Any
// other prompt completes immediately, so a second session can be shown to run while
// a first is paused.
const permissionPrompt = "raise-permission"

// panicPrompt is the prompt that makes a fixture session raise a request and then
// panic, so the deferred session-lock unlock can be shown to survive a panic while
// paused.
const panicPrompt = "raise-then-panic"

// permissionFactory builds sessions that raise one permission request per turn
// through the registry, and records every decision the raises received.
type permissionFactory struct {
	permissions interactive.PermissionRegistry

	mu        sync.Mutex
	decisions []claude.PermissionDecision
}

// Create returns a session that raises its permission requests through the registry.
func (f *permissionFactory) Create(_ string) agentlib.Session {
	return &permissionSession{factory: f}
}

// record appends the decision a raise received.
func (f *permissionFactory) record(decision claude.PermissionDecision) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.decisions = append(f.decisions, decision)
}

// Decisions returns a copy of the decisions the raises received.
func (f *permissionFactory) Decisions() []claude.PermissionDecision {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]claude.PermissionDecision(nil), f.decisions...)
}

// permissionSession raises one permission request per turn through the registry.
type permissionSession struct {
	factory *permissionFactory
}

// Prompt raises a permission request for the fixture prompts and answers the others.
func (s *permissionSession) Prompt(
	ctx context.Context,
	prompt string,
) (string, error) {
	switch prompt {
	case panicPrompt:
		_, _ = s.factory.permissions.DecidePermission(
			ctx,
			claude.PermissionRequest{ToolName: permissionTool},
		)
		panic("boom while paused")
	case permissionPrompt:
		decision, err := s.factory.permissions.DecidePermission(
			ctx,
			claude.PermissionRequest{ToolName: permissionTool},
		)
		if err != nil {
			return "", err
		}
		s.factory.record(decision)
		if !decision.Allow {
			return "", errors.New(decision.Message)
		}
		return "allowed", nil
	default:
		return "ok", nil
	}
}

// Close releases nothing: the fixture holds no process.
func (s *permissionSession) Close(_ context.Context) error {
	return nil
}

// pendingDTO decodes one element of the GET /permission array.
type pendingDTO struct {
	ID       string `json:"id"`
	ToolName string `json:"tool_name"`
}

// promptResult is the outcome of one prompt request run in the background.
type promptResult struct {
	status int
	body   string
	err    error
}

// verdictBody is the POST /permission request body.
type verdictBody struct {
	ID      string `json:"id"`
	Allow   bool   `json:"allow"`
	Message string `json:"message"`
}

// newPermissionTestServer builds a permission-enabled service and wraps its handler.
func newPermissionTestServer(
	permissions interactive.PermissionRegistry,
	factory agentlib.SessionFactory,
) *httptest.Server {
	svc := interactive.NewServiceWithPermissions(
		factory,
		":0",
		"",
		prometheus.NewRegistry(),
		permissions,
	)
	return httptest.NewServer(svc.Handler())
}

// getPermissions reads the pending requests from the endpoint.
func getPermissions(serverURL string) (int, []pendingDTO, string, error) {
	resp, err := http.Get(serverURL + "/permission")
	if err != nil {
		return 0, nil, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, "", err
	}
	var pending []pendingDTO
	if err := json.Unmarshal(raw, &pending); err != nil {
		return resp.StatusCode, nil, string(raw), err
	}
	return resp.StatusCode, pending, string(raw), nil
}

// postVerdict posts one verdict to the endpoint.
func postVerdict(serverURL, id string, allow bool, message string) (int, string, error) {
	payload, err := json.Marshal(verdictBody{ID: id, Allow: allow, Message: message})
	if err != nil {
		return 0, "", err
	}
	resp, err := http.Post(
		serverURL+"/permission",
		"application/json",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(raw), nil
}

// postPromptFull sends one prompt and returns the response status and body.
func postPromptFull(serverURL, sessionID, body string) (int, string, error) {
	req, err := http.NewRequest(http.MethodPost, serverURL+"/prompt", strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("X-Session-Id", sessionID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(raw), nil
}

// startPrompt runs one prompt in the background and returns the channel its outcome
// arrives on, so a test can assert on the pending request before draining it.
func startPrompt(serverURL, sessionID, body string) chan promptResult {
	out := make(chan promptResult, 1)
	go func() {
		status, respBody, err := postPromptFull(serverURL, sessionID, body)
		out <- promptResult{status: status, body: respBody, err: err}
	}()
	return out
}

// waitPending waits until the endpoint shows exactly one pending request and returns it.
func waitPending(serverURL string) pendingDTO {
	var pending []pendingDTO
	Eventually(func() int {
		_, list, _, err := getPermissions(serverURL)
		if err != nil {
			return -1
		}
		pending = list
		return len(list)
	}).Should(Equal(1))
	return pending[0]
}

var _ = Describe("Permission endpoint", func() {
	It("lists no pending requests on a permission-enabled service", func() {
		server := newPermissionTestServer(
			interactive.NewPermissionRegistry(),
			&mocks.SessionFactory{},
		)
		defer server.Close()

		status, pending, body, err := getPermissions(server.URL)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("[]"))
		Expect(pending).To(BeEmpty())
	})

	It("shows a pending request while its turn is blocked", func() {
		factory := &permissionFactory{permissions: interactive.NewPermissionRegistry()}
		server := newPermissionTestServer(factory.permissions, factory)
		defer server.Close()

		done := startPrompt(server.URL, "sess-a", permissionPrompt)
		pending := waitPending(server.URL)
		Expect(pending.ToolName).To(Equal(permissionTool))
		Expect(pending.ID).NotTo(BeEmpty())

		status, _, err := postVerdict(server.URL, pending.ID, true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Eventually(done).Should(Receive())
	})

	It("resolves a pending request with an allow verdict", func() {
		factory := &permissionFactory{permissions: interactive.NewPermissionRegistry()}
		server := newPermissionTestServer(factory.permissions, factory)
		defer server.Close()

		done := startPrompt(server.URL, "sess-a", permissionPrompt)
		pending := waitPending(server.URL)

		status, body, err := postVerdict(server.URL, pending.ID, true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("{}"))

		var result promptResult
		Eventually(done).Should(Receive(&result))
		Expect(result.err).To(BeNil())
		Expect(result.status).To(Equal(http.StatusOK))
		Expect(result.body).To(Equal("allowed"))
	})

	It("resolves a pending request with a deny verdict carrying the posted message", func() {
		factory := &permissionFactory{permissions: interactive.NewPermissionRegistry()}
		server := newPermissionTestServer(factory.permissions, factory)
		defer server.Close()

		done := startPrompt(server.URL, "sess-a", permissionPrompt)
		pending := waitPending(server.URL)

		status, body, err := postVerdict(server.URL, pending.ID, false, "not this time")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("{}"))

		var result promptResult
		Eventually(done).Should(Receive(&result))
		Expect(result.err).To(BeNil())
		Expect(result.status).To(Equal(http.StatusInternalServerError))
		Expect(result.body).To(Equal("prompt failed\n"))

		decisions := factory.Decisions()
		Expect(decisions).To(HaveLen(1))
		Expect(decisions[0].Allow).To(BeFalse())
		Expect(decisions[0].Message).To(Equal("not this time"))
	})

	It("refuses a verdict for an unknown id and creates nothing", func() {
		server := newPermissionTestServer(
			interactive.NewPermissionRegistry(),
			&mocks.SessionFactory{},
		)
		defer server.Close()

		status, _, err := postVerdict(server.URL, "no-such-id", true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusNotFound))

		status, pending, body, err := getPermissions(server.URL)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("[]"))
		Expect(pending).To(BeEmpty())
	})

	It("refuses a second verdict for the same id", func() {
		factory := &permissionFactory{permissions: interactive.NewPermissionRegistry()}
		server := newPermissionTestServer(factory.permissions, factory)
		defer server.Close()

		done := startPrompt(server.URL, "sess-a", permissionPrompt)
		pending := waitPending(server.URL)

		status, _, err := postVerdict(server.URL, pending.ID, true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Eventually(done).Should(Receive())

		status, _, err = postVerdict(server.URL, pending.ID, true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusNotFound))

		_, list, body, err := getPermissions(server.URL)
		Expect(err).To(BeNil())
		Expect(body).To(Equal("[]"))
		Expect(list).To(BeEmpty())
	})

	It("releases the waiter and leaves nothing pending when the turn is cancelled", func() {
		permissions := interactive.NewPermissionRegistry()
		server := newPermissionTestServer(permissions, &mocks.SessionFactory{})
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		errs := make(chan error, 1)
		go func() {
			_, err := permissions.DecidePermission(
				ctx,
				claude.PermissionRequest{ToolName: permissionTool},
			)
			errs <- err
		}()

		waitPending(server.URL)
		cancel()
		Eventually(errs).Should(Receive(Not(BeNil())))

		status, pending, body, err := getPermissions(server.URL)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("[]"))
		Expect(pending).To(BeEmpty())
	})

	It("does not block a prompt on a different session id", func() {
		factory := &permissionFactory{permissions: interactive.NewPermissionRegistry()}
		server := newPermissionTestServer(factory.permissions, factory)
		defer server.Close()

		done := startPrompt(server.URL, "sess-a", permissionPrompt)
		pending := waitPending(server.URL)

		status, body, err := postPromptFull(server.URL, "sess-b", "hello")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("ok"))

		_, list, _, err := getPermissions(server.URL)
		Expect(err).To(BeNil())
		Expect(list).To(HaveLen(1))

		status, _, err = postVerdict(server.URL, pending.ID, true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Eventually(done).Should(Receive())
	})

	It("does not share pending state between two services", func() {
		firstFactory := &permissionFactory{permissions: interactive.NewPermissionRegistry()}
		first := newPermissionTestServer(firstFactory.permissions, firstFactory)
		defer first.Close()
		second := newPermissionTestServer(
			interactive.NewPermissionRegistry(),
			&mocks.SessionFactory{},
		)
		defer second.Close()

		done := startPrompt(first.URL, "sess-a", permissionPrompt)
		pending := waitPending(first.URL)

		_, firstList, _, err := getPermissions(first.URL)
		Expect(err).To(BeNil())
		Expect(firstList).To(HaveLen(1))

		_, secondList, secondBody, err := getPermissions(second.URL)
		Expect(err).To(BeNil())
		Expect(secondBody).To(Equal("[]"))
		Expect(secondList).To(BeEmpty())

		status, _, err := postVerdict(first.URL, pending.ID, true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Eventually(done).Should(Receive())
	})

	It("resolves the endpoint and its sessions through one registry", func() {
		permissions := interactive.NewPermissionRegistry()
		factory := &permissionFactory{permissions: permissions}
		server := newPermissionTestServer(permissions, factory)
		defer server.Close()

		done := startPrompt(server.URL, "own-session", permissionPrompt)
		pending := waitPending(server.URL)
		Expect(pending.ToolName).To(Equal(permissionTool))

		status, _, err := postVerdict(server.URL, pending.ID, true, "")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Eventually(done).Should(Receive())
	})

	It("rejects a malformed verdict body", func() {
		server := newPermissionTestServer(
			interactive.NewPermissionRegistry(),
			&mocks.SessionFactory{},
		)
		defer server.Close()

		status, _, err := postRaw(server.URL, "not-json")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusBadRequest))

		status, _, err = postRaw(server.URL, `{"allow":true}`)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusBadRequest))
	})

	It("rejects a method other than GET or POST", func() {
		server := newPermissionTestServer(
			interactive.NewPermissionRegistry(),
			&mocks.SessionFactory{},
		)
		defer server.Close()

		req, err := http.NewRequest(http.MethodPut, server.URL+"/permission", nil)
		Expect(err).To(BeNil())
		resp, err := http.DefaultClient.Do(req)
		Expect(err).To(BeNil())
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		Expect(resp.StatusCode).To(Equal(http.StatusMethodNotAllowed))
	})

	It("releases the session lock when a paused turn panics", func() {
		factory := &permissionFactory{permissions: interactive.NewPermissionRegistry()}
		server := newPermissionTestServer(factory.permissions, factory)
		defer server.Close()

		done := startPrompt(server.URL, "sess-a", panicPrompt)
		pending := waitPending(server.URL)

		// The verdict releases the decider; the session then panics, so the client
		// sees a transport error rather than a response.
		_, _, _ = postVerdict(server.URL, pending.ID, true, "")
		Eventually(done).Should(Receive())

		status, body, err := postPromptFull(server.URL, "sess-a", "hello")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("ok"))
	})
})

// postRaw posts an arbitrary body to the permission endpoint.
func postRaw(serverURL, body string) (int, string, error) {
	resp, err := http.Post(
		serverURL+"/permission",
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, "", err
	}
	return resp.StatusCode, string(raw), nil
}
