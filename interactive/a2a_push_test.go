// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package interactive_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/push"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/agent/interactive"
	"github.com/bborbe/agent/mocks"
)

// pushNotificationTokenHeader is the header the SDK's sender carries the registered token
// in. It is spelled out here rather than imported, because the header name is part of the
// wire contract under test.
const pushNotificationTokenHeader = "A2A-Notification-Token"

// pushBody is the SDK's StreamResponse union as it arrives on the wire: exactly one of the
// four keys is present. The sender marshals a2a.StreamResponse{Event: event}, which is
// exactly this shape.
type pushBody struct {
	Task           *a2a.Task                    `json:"task"`
	StatusUpdate   *a2a.TaskStatusUpdateEvent   `json:"statusUpdate"`
	ArtifactUpdate *a2a.TaskArtifactUpdateEvent `json:"artifactUpdate"`
	Message        *a2a.Message                 `json:"message"`
}

// taskID reports the task the delivered event belongs to, or an empty string when the
// event names no task.
func (b pushBody) taskID() string {
	switch {
	case b.Task != nil:
		return string(b.Task.ID)
	case b.StatusUpdate != nil:
		return string(b.StatusUpdate.TaskID)
	case b.ArtifactUpdate != nil:
		return string(b.ArtifactUpdate.TaskID)
	}
	return ""
}

// pushPost is one request the receiver recorded.
type pushPost struct {
	receivedAt time.Time
	token      string
	body       pushBody
}

// pushReceiver is a loopback receiver that records every POST it receives: when it
// arrived, the token header and the decoded body. It answers 200 to every request.
type pushReceiver struct {
	server *httptest.Server
	mu     sync.Mutex
	posts  []pushPost
}

// newPushReceiver starts a loopback receiver.
func newPushReceiver() *pushReceiver {
	receiver := &pushReceiver{}
	receiver.server = httptest.NewServer(http.HandlerFunc(func(
		w http.ResponseWriter,
		r *http.Request,
	) {
		raw, err := io.ReadAll(r.Body)
		Expect(err).To(BeNil())
		var body pushBody
		Expect(json.Unmarshal(raw, &body)).To(Succeed())
		receiver.mu.Lock()
		receiver.posts = append(receiver.posts, pushPost{
			receivedAt: time.Now(),
			token:      r.Header.Get(pushNotificationTokenHeader),
			body:       body,
		})
		receiver.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	return receiver
}

// URL is the receiver's own address, which is the callback URL a spec registers.
func (r *pushReceiver) URL() string { return r.server.URL }

// Posts returns a snapshot of every request recorded so far.
func (r *pushReceiver) Posts() []pushPost {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]pushPost(nil), r.posts...)
}

// Close stops the receiver.
func (r *pushReceiver) Close() { r.server.Close() }

// postsForTask filters a snapshot down to the posts naming taskID.
func postsForTask(posts []pushPost, taskID string) []pushPost {
	var matching []pushPost
	for _, post := range posts {
		if post.body.taskID() == taskID {
			matching = append(matching, post)
		}
	}
	return matching
}

// newPushService builds a push-wired service and wraps its handler in an httptest server,
// returning the receiver the caller registers as its callback. The test builds its own
// sender because a loopback receiver is a blocked target under the SDK's default SSRF
// guard. The receiver URL is the one each spec registers, so the sender needs no URL.
func newPushService(factory agentlib.SessionFactory) (*httptest.Server, *pushReceiver) {
	receiver := newPushReceiver()
	svc := interactive.NewService(
		factory,
		":0",
		"",
		prometheus.NewRegistry(),
		interactive.AuthDisabled,
		interactive.CardConfig{PublicURL: testPublicURL},
		interactive.DefaultSessionIdleTimeout,
		interactive.DefaultMaxSessions,
		interactive.WithPushNotifications(
			push.NewInMemoryStore(),
			push.NewHTTPPushSender(&push.HTTPSenderConfig{AllowPrivateNetworks: true}),
		),
	)
	return httptest.NewServer(svc.Handler()), receiver
}

// pushCapability reads the served Agent Card and returns the value of
// capabilities.pushNotifications.
func pushCapability(serverURL string) bool {
	status, body, err := getAgentCard(serverURL)
	Expect(err).To(BeNil())
	Expect(status).To(Equal(http.StatusOK))

	var card map[string]any
	Expect(json.Unmarshal([]byte(body), &card)).To(Succeed())
	capabilities, ok := card["capabilities"].(map[string]any)
	Expect(ok).To(BeTrue(), "the served card must carry a capabilities object")
	GinkgoWriter.Printf("card capabilities: %v", capabilities)
	value, ok := capabilities["pushNotifications"].(bool)
	Expect(ok).To(BeTrue(), "capabilities.pushNotifications must be a bool")
	return value
}

// createTask runs one plain SendMessage and returns the id of the task it created.
func createTask(serverURL string) string {
	status, body, err := postA2A(serverURL, "", a2aSendBody(1, "abc", "hello"))
	Expect(err).To(BeNil())
	Expect(status).To(Equal(http.StatusOK))

	task, err := taskFromResult(body)
	Expect(err).To(BeNil())
	return string(task.ID)
}

// a2aSendBodyWithPushConfig builds a JSON-RPC SendMessage request carrying one text part
// and a message-level push configuration. The SDK saves that config against the task id
// before the turn runs, which makes the turn's own submitted-task event deliverable.
func a2aSendBodyWithPushConfig(
	id int,
	contextID, text, callbackURL, token string,
) string {
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
		"params": map[string]any{
			"message": message,
			"configuration": map[string]any{
				"taskPushNotificationConfig": map[string]any{
					"url":   callbackURL,
					"token": token,
				},
			},
		},
	})
	Expect(err).To(BeNil())
	return string(body)
}

// a2aCreatePushConfigBody builds a CreateTaskPushNotificationConfig request. That method's
// params ARE an a2a.PushConfig, so taskId, url and token are siblings.
func a2aCreatePushConfigBody(id int, taskID, callbackURL, token string) string {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "CreateTaskPushNotificationConfig",
		"params": map[string]any{
			"taskId": taskID,
			"url":    callbackURL,
			"token":  token,
		},
	})
	Expect(err).To(BeNil())
	return string(body)
}

// a2aListPushConfigsBody builds a ListTaskPushNotificationConfigs request.
func a2aListPushConfigsBody(id int, taskID string) string {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "ListTaskPushNotificationConfigs",
		"params":  map[string]any{"taskId": taskID},
	})
	Expect(err).To(BeNil())
	return string(body)
}

// pushConfigFromResult decodes a single push configuration out of the envelope's result.
func pushConfigFromResult(body string) (*a2a.PushConfig, error) {
	var envelope a2aEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return nil, err
	}
	var config a2a.PushConfig
	if err := json.Unmarshal(envelope.Result, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// pushConfigsFromResult decodes a push-config listing out of the envelope's result.
func pushConfigsFromResult(body string) (*a2a.ListTaskPushConfigResponse, error) {
	var envelope a2aEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return nil, err
	}
	var list a2a.ListTaskPushConfigResponse
	if err := json.Unmarshal(envelope.Result, &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// newPushServiceFactory builds the service under construction for the fail-loud specs, so
// the panic assertion wraps the constructor call rather than the option constructor alone.
func constructServiceWith(option interactive.ServiceOption) func() {
	return func() {
		interactive.NewService(
			newAuthMockFactory(),
			":0",
			"",
			prometheus.NewRegistry(),
			interactive.AuthDisabled,
			interactive.CardConfig{PublicURL: testPublicURL},
			interactive.DefaultSessionIdleTimeout,
			interactive.DefaultMaxSessions,
			option,
		)
	}
}

var _ = Describe("A2A push notifications", func() {
	It("advertises push notifications when the caller wired them", func() {
		server, receiver := newPushService(newAuthMockFactory())
		defer server.Close()
		defer receiver.Close()

		Expect(pushCapability(server.URL)).To(BeTrue())

		status, body, err := getAgentCard(server.URL)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		var card map[string]any
		Expect(json.Unmarshal([]byte(body), &card)).To(Succeed())
		capabilities, ok := card["capabilities"].(map[string]any)
		Expect(ok).To(BeTrue())
		Expect(capabilities["streaming"]).To(Equal(true))
	})

	It("advertises no push and refuses a registration when the caller wired nothing", func() {
		// The receiver is this negative's receiver, and nothing may ever reach it.
		receiver := newPushReceiver()
		defer receiver.Close()

		server := newA2ATestServer(
			testPublicURL,
			interactive.AuthDisabled,
			newAuthMockFactory(),
		)
		defer server.Close()

		Expect(pushCapability(server.URL)).To(BeFalse())

		status, body, err := postA2A(
			server.URL,
			"",
			a2aCreatePushConfigBody(1, "task-1", receiver.URL(), "tok-1"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		var envelope a2aEnvelope
		Expect(json.Unmarshal([]byte(body), &envelope)).To(Succeed())
		Expect(envelope.Error).NotTo(BeNil())
		Expect(envelope.Error.Code).To(Equal(-32003))
		Expect(envelope.Error.Message).To(ContainSubstring("push notification not supported"))
		Expect(body).NotTo(ContainSubstring(`"result"`))
		Expect(receiver.Posts()).To(BeEmpty())
	})

	It("drives the card and the registration from one decision", func() {
		// Unconfigured: card false, registration refused.
		plain := newA2ATestServer(testPublicURL, interactive.AuthDisabled, newAuthMockFactory())
		defer plain.Close()

		plainCard := pushCapability(plain.URL)
		plainTaskID := createTask(plain.URL)
		plainStatus, plainBody, err := postA2A(
			plain.URL,
			"",
			a2aCreatePushConfigBody(2, plainTaskID, "https://callback.example.test/hook", "tok-x"),
		)
		Expect(err).To(BeNil())
		Expect(plainStatus).To(Equal(http.StatusOK))
		plainCode, plainPresent := a2aErrorCode(plainBody)
		Expect(plainPresent).To(BeTrue())
		Expect(plainCode).To(Equal(-32003))
		GinkgoWriter.Printf(
			"unconfigured run: card=%v registrationCode=%d", plainCard, plainCode,
		)

		// Configured: card true, registration accepted.
		pushServer, receiver := newPushService(newAuthMockFactory())
		defer pushServer.Close()
		defer receiver.Close()

		pushCard := pushCapability(pushServer.URL)
		pushTaskID := createTask(pushServer.URL)
		pushStatus, pushBody, err := postA2A(
			pushServer.URL,
			"",
			a2aCreatePushConfigBody(3, pushTaskID, receiver.URL(), "tok-3"),
		)
		Expect(err).To(BeNil())
		Expect(pushStatus).To(Equal(http.StatusOK))
		config, err := pushConfigFromResult(pushBody)
		Expect(err).To(BeNil())
		Expect(config.URL).To(Equal(receiver.URL()))
		Expect(config.Token).To(Equal("tok-3"))
		GinkgoWriter.Printf(
			"configured run: card=%v registrationURL=%s", pushCard, config.URL,
		)

		Expect(plainCard).To(BeFalse())
		Expect(pushCard).To(BeTrue())
		Expect(plainCard).NotTo(Equal(pushCard))
	})

	It("round-trips a registration for an existing task and refuses an unknown one", func() {
		server, receiver := newPushService(newAuthMockFactory())
		defer server.Close()
		defer receiver.Close()

		taskID := createTask(server.URL)

		status, body, err := postA2A(
			server.URL, "", a2aCreatePushConfigBody(2, taskID, receiver.URL(), "tok-1"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		config, err := pushConfigFromResult(body)
		Expect(err).To(BeNil())
		Expect(config.URL).To(Equal(receiver.URL()))
		Expect(config.Token).To(Equal("tok-1"))

		status, body, err = postA2A(server.URL, "", a2aListPushConfigsBody(3, taskID))
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		list, err := pushConfigsFromResult(body)
		Expect(err).To(BeNil())
		GinkgoWriter.Printf("registered configs for task %s: %d", taskID, len(list.Configs))
		Expect(list.Configs).To(HaveLen(1))
		Expect(list.Configs[0].URL).To(Equal(receiver.URL()))
		Expect(list.Configs[0].Token).To(Equal("tok-1"))

		const unknownTaskID = "task-the-service-never-saw"
		status, body, err = postA2A(
			server.URL,
			"",
			a2aCreatePushConfigBody(4, unknownTaskID, receiver.URL(), "tok-2"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).NotTo(ContainSubstring(`"result"`))
		code, present := a2aErrorCode(body)
		Expect(present).To(BeTrue())
		// The refusal is the task-store one, not the push-support one.
		Expect(code).NotTo(Equal(-32003))
		Expect(postsForTask(receiver.Posts(), unknownTaskID)).To(BeEmpty())
	})

	It("delivers a turn's events to the registered callback through the caller's sender", func() {
		server, receiver := newPushService(newAuthMockFactory())
		defer server.Close()
		defer receiver.Close()

		status, body, err := postA2A(
			server.URL,
			"",
			a2aSendBodyWithPushConfig(1, "abc", "hello", receiver.URL(), "tok-live"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		task, err := taskFromResult(body)
		Expect(err).To(BeNil())

		var posts []pushPost
		Eventually(func() int {
			posts = postsForTask(receiver.Posts(), string(task.ID))
			return len(posts)
		}, 5*time.Second).Should(BeNumerically(">=", 1))
		GinkgoWriter.Printf("delivered posts for task %s: %d", task.ID, len(posts))

		var sawSubmittedTask bool
		for _, post := range posts {
			Expect(post.token).To(Equal("tok-live"))
			if post.body.Task != nil {
				sawSubmittedTask = true
			}
		}
		Expect(sawSubmittedTask).To(BeTrue(), "the submitted-task event must be delivered")

		// The completion event must carry a message, so a receiver has a messageId to
		// record. The mid-turn working event carries one too, which is why this indexes
		// the completed entry specifically rather than any event that happens to have an
		// id: the assertion has to fail on the nil message it was written against.
		var completionMessageID string
		Eventually(func() string {
			completionMessageID = ""
			for _, post := range postsForTask(receiver.Posts(), string(task.ID)) {
				if post.body.StatusUpdate == nil ||
					post.body.StatusUpdate.Status.State != a2a.TaskStateCompleted {
					continue
				}
				if msg := post.body.StatusUpdate.Status.Message; msg != nil {
					completionMessageID = msg.ID
				}
			}
			return completionMessageID
		}, 5*time.Second).ShouldNot(BeEmpty(),
			"the completion event's message must carry a non-empty messageId")
	})

	It("delivers nothing when the caller wired nothing", func() {
		receiver := newPushReceiver()
		defer receiver.Close()

		server := newA2ATestServer(
			testPublicURL,
			interactive.AuthDisabled,
			newAuthMockFactory(),
		)
		defer server.Close()

		status, body, err := postA2A(
			server.URL,
			"",
			a2aSendBodyWithPushConfig(1, "abc", "hello", receiver.URL(), "tok-none"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))

		var envelope a2aEnvelope
		Expect(json.Unmarshal([]byte(body), &envelope)).To(Succeed())
		Expect(envelope.Error).NotTo(BeNil())
		Expect(envelope.Error.Message).To(ContainSubstring("push notification not supported"))
		Expect(body).NotTo(ContainSubstring(`"result"`))
		Expect(receiver.Posts()).To(BeEmpty())
	})

	It("fails loudly at construction when the option is half supplied", func() {
		Expect(constructServiceWith(
			interactive.WithPushNotifications(push.NewInMemoryStore(), nil),
		)).To(PanicWith(ContainSubstring("push sender")))

		Expect(constructServiceWith(
			interactive.WithPushNotifications(
				nil,
				push.NewHTTPPushSender(&push.HTTPSenderConfig{AllowPrivateNetworks: true}),
			),
		)).To(PanicWith(ContainSubstring("push config store")))
	})
})

// turnLineInstant returns the instant glog stamped on the stderr line containing marker. glog's
// stderr header is `[IWEF]mmdd hh:mm:ss.uuuuuu threadid file:line] msg` (documented in
// github.com/golang/glog, which builds it as the severity letter followed by mmdd, a space, and
// hh:mm:ss.uuuuuu), so the timestamp is the 20 characters starting at index 1 of the line. The
// header carries no year, so the year is taken from now, and the location is time.Local — which
// this suite fixes to UTC in TestInteractive.
func turnLineInstant(out, marker string, now time.Time) time.Time {
	index := strings.Index(out, marker)
	Expect(index).To(
		BeNumerically(">=", 0),
		"the captured log must carry the marker %q",
		marker,
	)
	line := out[strings.LastIndex(out[:index], "\n")+1:]
	Expect(len(line)).To(
		BeNumerically(">=", 21),
		"the line carrying %q must carry a glog timestamp header",
		marker,
	)
	parsed, err := time.Parse("0102 15:04:05.000000", line[1:21])
	Expect(err).To(BeNil(), "the glog header of %q must parse", marker)
	return time.Date(
		now.Year(),
		parsed.Month(),
		parsed.Day(),
		parsed.Hour(),
		parsed.Minute(),
		parsed.Second(),
		parsed.Nanosecond(),
		time.Local,
	)
}

// sleepingFactory builds sessions whose every turn sleeps work and then returns result, so a
// spec can hold a turn open for longer than the mid-turn delay.
func sleepingFactory(work time.Duration, result string) *mocks.SessionFactory {
	session := &mocks.Session{}
	session.PromptStub = func(_ context.Context, _ string) (string, error) {
		time.Sleep(work)
		return result, nil
	}
	factory := &mocks.SessionFactory{}
	factory.CreateReturns(session)
	return factory
}

// postsWithStatusState filters a snapshot down to the posts whose status-update event carries
// state. The decoded state is the SDK's wire constant (`TASK_STATE_WORKING`), not the short
// name, so the comparison is made against the SDK's typed constants rather than a hand-written
// string.
func postsWithStatusState(posts []pushPost, state string) []pushPost {
	var matching []pushPost
	for _, post := range posts {
		if post.body.StatusUpdate == nil {
			continue
		}
		if string(post.body.StatusUpdate.Status.State) == state {
			matching = append(matching, post)
		}
	}
	return matching
}

// sawSubmittedTask reports whether any post in the snapshot carries the submitted-task event.
func sawSubmittedTask(posts []pushPost) bool {
	for _, post := range posts {
		if post.body.Task != nil {
			return true
		}
	}
	return false
}

var _ = Describe("A2A mid-turn status", func() {
	It("yields one working status mid-turn without lengthening the turn", func() {
		const (
			sessionID = "midturn-long"
			// turnWork holds the turn open past the delay, so the mid-turn event lands
			// inside both timing bounds.
			turnWork = 31 * time.Second
			// windowFloor is 9 s, not the 10 s delay: turn start is emitted on the turn's
			// own goroutine while the delay timer is armed on the executor's goroutine, so
			// the two instants are separated by sub-second scheduling skew. 9 s still
			// excludes an event emitted without waiting at all.
			windowFloor = 9 * time.Second
			// windowCeil is the delay itself: the event must still be 10 s before the end.
			windowCeil = 10 * time.Second
			// turnCeiling pins the work time plus 5 s of slack, so a concurrent
			// implementation passes while a serial sleep-then-work one fails.
			turnCeiling = 36 * time.Second
		)

		server, receiver := newPushService(sleepingFactory(turnWork, "slow-result"))
		defer server.Close()
		defer receiver.Close()

		now := time.Now()
		var status int
		var body string
		var err error
		out := captureStderr(func() {
			status, body, err = postA2A(
				server.URL,
				"",
				a2aSendBodyWithPushConfig(1, sessionID, "hello", receiver.URL(), "tok-mid"),
			)
		})
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		task, err := taskFromResult(body)
		Expect(err).To(BeNil())

		var posts []pushPost
		Eventually(func() int {
			posts = postsForTask(receiver.Posts(), string(task.ID))
			return len(postsWithStatusState(posts, string(a2a.TaskStateWorking))) +
				len(postsWithStatusState(posts, string(a2a.TaskStateCompleted)))
		}, 5*time.Second).Should(BeNumerically(">=", 2))

		// AC5 — the by-state pair, selected by state rather than by POST count.
		working := postsWithStatusState(posts, string(a2a.TaskStateWorking))
		completed := postsWithStatusState(posts, string(a2a.TaskStateCompleted))
		Expect(working).To(HaveLen(1), "the turn must yield exactly one working status")
		Expect(completed).To(HaveLen(1))

		// AC6 — the working status carries non-empty message text.
		message := working[0].body.StatusUpdate.Status.Message
		Expect(message).NotTo(BeNil(), "the working status must carry a message")
		texts := make([]string, 0, len(message.Parts))
		for _, part := range message.Parts {
			texts = append(texts, part.Text())
		}
		Expect(texts).NotTo(BeEmpty(), "the message must carry at least one part")
		Expect(strings.Join(texts, "")).NotTo(BeEmpty(), "the message text must be non-empty")

		// AC7 — the window, measured against the service's own turn-boundary lines.
		turnStart := turnLineInstant(out, "turn start id="+sessionID, now)
		turnEnd := turnLineInstant(out, "turn end id="+sessionID, now)
		workingAt := working[0].receivedAt
		GinkgoWriter.Printf(
			"mid-turn timings: working-start=%s end-working=%s turn=%s",
			workingAt.Sub(turnStart),
			turnEnd.Sub(workingAt),
			turnEnd.Sub(turnStart),
		)
		Expect(workingAt.Sub(turnStart)).To(BeNumerically(">=", windowFloor))
		Expect(turnEnd.Sub(workingAt)).To(BeNumerically(">=", windowCeil))
		Expect(turnEnd.Sub(turnStart)).To(BeNumerically("<", turnCeiling))
	})

	It("emits no mid-turn status when the turn ends before the delay", func() {
		const sessionID = "midturn-fast"
		server, receiver := newPushService(newAuthMockFactory())
		defer server.Close()
		defer receiver.Close()

		status, body, err := postA2A(
			server.URL,
			"",
			a2aSendBodyWithPushConfig(1, sessionID, "hello", receiver.URL(), "tok-fast"),
		)
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		task, err := taskFromResult(body)
		Expect(err).To(BeNil())

		// Wait for the terminal event: pushes are sent in order, so a working-state POST
		// would have to have been recorded before the completion this waits on.
		var posts []pushPost
		Eventually(func() int {
			posts = postsForTask(receiver.Posts(), string(task.ID))
			return len(postsWithStatusState(posts, string(a2a.TaskStateCompleted)))
		}, 5*time.Second).Should(Equal(1))

		Expect(postsWithStatusState(posts, string(a2a.TaskStateWorking))).To(BeEmpty())
		Expect(sawSubmittedTask(posts)).To(
			BeTrue(),
			"the submitted-task event must still be delivered",
		)
	})

	It("fails the turn rather than the process when the backend panics", func() {
		var calls int32
		session := &mocks.Session{}
		session.PromptStub = func(_ context.Context, _ string) (string, error) {
			if atomic.AddInt32(&calls, 1) == 1 {
				panic("boom inside the turn")
			}
			return "recovered", nil
		}
		factory := &mocks.SessionFactory{}
		factory.CreateReturns(session)
		server := newA2ATestServer(testPublicURL, interactive.AuthDisabled, factory)
		defer server.Close()

		status, body, err := postA2A(server.URL, "", a2aSendBody(1, "panic-session", "one"))
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		task, err := taskFromResult(body)
		Expect(err).To(BeNil())
		Expect(task.Status.State).To(Equal(a2a.TaskStateFailed))

		// The panic unwound through the deferred unlock, so the next turn on the same
		// session id serves normally.
		status, body, err = postA2A(server.URL, "", a2aSendBody(2, "panic-session", "two"))
		Expect(err).To(BeNil())
		Expect(status).To(Equal(http.StatusOK))
		task, err = taskFromResult(body)
		Expect(err).To(BeNil())
		Expect(task.Status.State).To(Equal(a2a.TaskStateCompleted))
	})
})
