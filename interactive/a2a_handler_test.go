// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/bborbe/errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/interactive"
	"github.com/bborbe/agent/mocks"
)

// a2aErrorBody is the JSON-RPC error object a failed call carries.
type a2aErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// a2aEnvelope is the JSON-RPC response envelope: exactly one of Result and Error is
// present. It is decoded here rather than imported, because the wire shape is the
// contract under test.
type a2aEnvelope struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *a2aErrorBody   `json:"error"`
}

// postA2A sends one JSON-RPC body to /a2a and returns the status and the response body.
// An empty authorization sends no Authorization header at all, which is the "absent
// header" case the gate refuses.
func postA2A(serverURL, authorization, body string) (int, string, error) {
	req, err := http.NewRequest(http.MethodPost, serverURL+"/a2a", strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
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

// a2aSendBody builds a JSON-RPC SendMessage request carrying one text part. The
// contextId field is included only when contextID is non-empty: an absent field is the
// "no conversation named" case, which resolves to the default conversation.
func a2aSendBody(id int, contextID, text string) string {
	message := map[string]any{
		"messageId": "m1",
		"role":      "ROLE_USER",
		"parts":     []map[string]string{{"text": text}},
	}
	if contextID != "" {
		message["contextId"] = contextID
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "SendMessage",
		"params":  map[string]any{"message": message},
	})
	Expect(err).To(BeNil())
	return string(body)
}

// a2aMethodBody builds a JSON-RPC request for an arbitrary method name, so the
// unimplemented-method case can be exercised.
func a2aMethodBody(id int, method string) string {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	})
	Expect(err).To(BeNil())
	return string(body)
}

// a2aRawPartsBody builds a JSON-RPC SendMessage request whose `parts` array is emitted
// verbatim, so a parts list the typed helpers cannot express — notably one carrying a
// literal JSON `null` element — can be exercised. contextID must be a plain id; it is
// interpolated into the message rather than marshalled.
func a2aRawPartsBody(id int, contextID, partsJSON string) string {
	message := `{"messageId":"m1","role":"ROLE_USER","contextId":"` + contextID +
		`","parts":` + partsJSON + `}`
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "SendMessage",
		"params":  map[string]any{"message": json.RawMessage(message)},
	})
	Expect(err).To(BeNil())
	return string(body)
}

// a2aErrorCode decodes a JSON-RPC response envelope and reports the error code and
// whether an error object is present.
func a2aErrorCode(body string) (int, bool) {
	var envelope a2aEnvelope
	Expect(json.Unmarshal([]byte(body), &envelope)).To(Succeed())
	if envelope.Error == nil {
		return 0, false
	}
	return envelope.Error.Code, true
}

// taskFromResult decodes the envelope's result into a stream response and returns the
// task it carries.
func taskFromResult(body string) (*a2a.Task, error) {
	var envelope a2aEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return nil, errors.Wrap(context.Background(), err, "unmarshal envelope failed")
	}
	var response a2a.StreamResponse
	if err := json.Unmarshal(envelope.Result, &response); err != nil {
		return nil, errors.Wrap(context.Background(), err, "unmarshal stream response failed")
	}
	task, ok := response.Event.(*a2a.Task)
	if !ok {
		return nil, errors.Errorf(context.Background(), "result is not a task")
	}
	return task, nil
}

// postPromptFullAuth is postPromptFull with an Authorization header, so the native
// route can serve as the reference against an auth-enabled server. It is identical to
// postPromptFull except that it sets the header when authorization is non-empty.
func postPromptFullAuth(serverURL, sessionID, body, authorization string) (int, string, error) {
	req, err := http.NewRequest(http.MethodPost, serverURL+"/prompt", strings.NewReader(body))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("X-Session-Id", sessionID)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
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

// echoingFactory builds sessions that echo their input, so an equality assertion proves
// the request text reached the session rather than merely that some reply came back.
func echoingFactory() *mocks.SessionFactory {
	factory := &mocks.SessionFactory{}
	factory.CreateStub = func(_ string) agentlib.Session {
		session := &mocks.Session{}
		session.PromptStub = func(_ context.Context, prompt string) (string, error) {
			return "echo:" + prompt, nil
		}
		return session
	}
	return factory
}

var _ = Describe("A2A endpoint", func() {
	It("returns the agent's own computed result as a completed task", func() {
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			echoingFactory(),
		)
		defer server.Close()

		nativeStatus, nativeBody, err := postPromptFullAuth(
			server.URL, "abc", "hello", "Bearer "+authTestToken,
		)
		Expect(err).To(BeNil())
		Expect(nativeStatus).To(Equal(http.StatusOK))

		status, body, err := postA2A(
			server.URL, "Bearer "+authTestToken, a2aSendBody(1, "abc", "hello"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		task, err := taskFromResult(body)
		Expect(err).To(BeNil())
		Expect(task.Status.State).To(Equal(a2a.TaskStateCompleted))
		Expect(task.Artifacts).To(HaveLen(1))
		Expect(task.Artifacts[0].Parts[0].Text()).To(Equal(nativeBody))
		Expect(nativeBody).To(Equal("echo:hello"))
	})

	It("refuses an unauthenticated request with 401", func() {
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			newAuthMockFactory(),
		)
		defer server.Close()

		status, _, err := postA2A(server.URL, "", a2aSendBody(1, "abc", "hello"))
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))

		status, _, err = postA2A(
			server.URL, "Bearer "+authWrongToken, a2aSendBody(2, "abc", "hello"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusUnauthorized))
	})

	It("serves an authenticated request with 200", func() {
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			newAuthMockFactory(),
		)
		defer server.Close()

		status, _, err := postA2A(
			server.URL, "Bearer "+authTestToken, a2aSendBody(3, "abc", "hello"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
	})

	It("serialises two calls on one contextId", func() {
		release := make(chan struct{})
		started := make(chan struct{}, 2)
		factory := &mocks.SessionFactory{}
		factory.CreateStub = func(_ string) agentlib.Session {
			session := &mocks.Session{}
			session.PromptStub = func(_ context.Context, _ string) (string, error) {
				started <- struct{}{}
				<-release
				return "ok", nil
			}
			return session
		}
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			factory,
		)
		defer server.Close()

		statuses := make([]int, 2)
		errs := make([]error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				statuses[i], _, errs[i] = postA2A(
					server.URL,
					"Bearer "+authTestToken,
					a2aSendBody(i+1, "abc", "hello"),
				)
			}(i)
		}

		Eventually(started).Should(Receive())
		Consistently(started, 200*time.Millisecond).ShouldNot(Receive())
		Expect(factory.CreateCallCount()).To(Equal(1))

		close(release)
		wg.Wait()
		Eventually(started).Should(Receive())

		Expect(errs[0]).To(BeNil())
		Expect(errs[1]).To(BeNil())
		Expect(statuses).To(Equal([]int{http.StatusOK, http.StatusOK}))
	})

	It("resolves an absent contextId to the identity conversation", func() {
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			newAuthMockFactory(),
		)
		defer server.Close()

		var status int
		var err error
		out := captureStderr(func() {
			status, _, err = postA2A(
				server.URL, "Bearer "+authTestToken, a2aSendBody(1, "", "hello"),
			)
		})
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(out).To(ContainSubstring("turn start id=identity"))
	})

	It("shares the session cache with the native route", func() {
		factory := echoingFactory()
		server := newA2ATestServer(testPublicURL, interactive.AuthDisabled, factory)
		defer server.Close()

		status, _, err := postPromptFull(server.URL, "abc", "hello")
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(factory.CreateCallCount()).To(Equal(1))

		status, _, err = postA2A(server.URL, "", a2aSendBody(1, "abc", "hello"))
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(factory.CreateCallCount()).To(Equal(1))
	})

	It("rejects a contextId that fails the pattern before building a session", func() {
		for _, contextID := range []string{"-x", "a/b"} {
			factory := &mocks.SessionFactory{}
			factory.CreateReturns(&mocks.Session{})
			server := newA2ATestServer(
				testPublicURL,
				interactive.NewAuthToken(authTestToken),
				factory,
			)

			var status int
			var body string
			var err error
			out := captureStderr(func() {
				status, body, err = postA2A(
					server.URL,
					"Bearer "+authTestToken,
					a2aSendBody(1, contextID, "hello"),
				)
			})
			Expect(err).To(BeNil())
			Expect(status).To(Equal(http.StatusOK))

			code, present := a2aErrorCode(body)
			Expect(present).To(BeTrue())
			Expect(code).To(Equal(-32602))
			Expect(body).NotTo(ContainSubstring(`"result"`))
			Expect(out).NotTo(ContainSubstring("turn start"))
			Expect(factory.CreateCallCount()).To(Equal(0))

			server.Close()
		}
	})

	It("reports an unimplemented method as a JSON-RPC error", func() {
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			newAuthMockFactory(),
		)
		defer server.Close()

		status, body, err := postA2A(
			server.URL, "Bearer "+authTestToken, a2aMethodBody(1, "not/a-real-method"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		code, present := a2aErrorCode(body)
		Expect(present).To(BeTrue())
		Expect(code).To(Equal(-32601))
		Expect(body).NotTo(ContainSubstring(`"result"`))
	})

	It("reports a backend failure as a failed task without leaking the error", func() {
		session := &mocks.Session{}
		session.PromptReturns("", stderrors.New("backend exploded"))
		factory := &mocks.SessionFactory{}
		factory.CreateReturns(session)
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			factory,
		)
		defer server.Close()

		var status int
		var body string
		var err error
		out := captureStderr(func() {
			status, body, err = postA2A(
				server.URL, "Bearer "+authTestToken, a2aSendBody(1, "abc", "hello"),
			)
		})
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		task, err := taskFromResult(body)
		Expect(err).To(BeNil())
		Expect(task.Status.State).To(Equal(a2a.TaskStateFailed))
		Expect(out).To(ContainSubstring("backend exploded"))
		Expect(body).NotTo(ContainSubstring("backend exploded"))
	})

	It("refuses a parts array carrying a null element without building a session", func() {
		// The wire format allows a literal `null` inside `parts`, and the SDK decodes
		// `[]*Part` so such an element arrives as a nil pointer. (*Part).Text()
		// dereferences its receiver, so this body is the regression case for the
		// handler panicking on malformed-but-valid JSON.
		factory := &mocks.SessionFactory{}
		factory.CreateReturns(&mocks.Session{})
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			factory,
		)
		defer server.Close()

		var status int
		var body string
		var err error
		out := captureStderr(func() {
			status, body, err = postA2A(
				server.URL,
				"Bearer "+authTestToken,
				a2aRawPartsBody(1, "abc", "[null]"),
			)
		})
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		code, present := a2aErrorCode(body)
		Expect(present).To(BeTrue())
		Expect(code).To(Equal(-32602))
		Expect(body).NotTo(ContainSubstring(`"result"`))
		Expect(out).NotTo(ContainSubstring("turn start"))
		Expect(factory.CreateCallCount()).To(Equal(0))
	})

	It("refuses a body larger than the cap without building a session", func() {
		// The 1 MiB cap is the route's only body control. Unlike /prompt — which
		// truncates via io.LimitReader — an oversized body here is refused, because a
		// truncated JSON body cannot parse.
		factory := &mocks.SessionFactory{}
		factory.CreateReturns(&mocks.Session{})
		server := newA2ATestServer(
			testPublicURL,
			interactive.NewAuthToken(authTestToken),
			factory,
		)
		defer server.Close()

		oversized := a2aSendBody(1, "abc", strings.Repeat("x", 2<<20))

		var status int
		var body string
		var err error
		out := captureStderr(func() {
			status, body, err = postA2A(server.URL, "Bearer "+authTestToken, oversized)
		})
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).NotTo(ContainSubstring(`"result"`))
		Expect(out).NotTo(ContainSubstring("turn start"))
		Expect(factory.CreateCallCount()).To(Equal(0))
	})
})
