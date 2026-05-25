// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package result

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"
	"github.com/golang/glog"
	"gopkg.in/yaml.v3"

	"github.com/bborbe/agent/lib"
	gitclient "github.com/bborbe/agent/task/controller/pkg/gitrestclient"
	"github.com/bborbe/agent/task/controller/pkg/metrics"
)

//counterfeiter:generate -o ../../mocks/result_writer.go --fake-name ResultWriter . ResultWriter

// ResultWriter writes a Task back to the vault task file.
type ResultWriter interface {
	WriteResult(ctx context.Context, req lib.Task) error
}

// NewResultWriter creates a ResultWriter that locates task files in the vault
// and writes the result, committing via gitClient.
func NewResultWriter(
	gitClient gitclient.GitClient,
	taskDir string,
	currentDateTime libtime.CurrentDateTimeGetter,
) ResultWriter {
	return &resultWriter{
		gitClient:       gitClient,
		taskDir:         taskDir,
		currentDateTime: currentDateTime,
	}
}

type resultWriter struct {
	gitClient       gitclient.GitClient
	taskDir         string
	currentDateTime libtime.CurrentDateTimeGetter
}

// FindTaskFilePath lists files in taskDir via gitClient and returns the relative path of
// the .md file whose frontmatter has task_identifier == id, plus the parsed existing frontmatter.
// Returns ("", nil, nil) when no match is found (not an error).
func FindTaskFilePath(
	ctx context.Context,
	gitClient gitclient.GitClient,
	taskDir string,
	id lib.TaskIdentifier,
) (string, lib.TaskFrontmatter, error) {
	glob := taskDir + "/*.md"
	paths, err := gitClient.ListFiles(ctx, glob)
	if err != nil {
		return "", nil, errors.Wrapf(ctx, err, "list task files with glob %s", glob)
	}
	var matchedRelPath string
	var existingFrontmatter lib.TaskFrontmatter
	for _, relPath := range paths {
		content, readErr := gitClient.ReadFile(ctx, relPath)
		if readErr != nil {
			glog.V(3).Infof("FindTaskFilePath: skip %s (read error: %v)", relPath, readErr)
			continue
		}
		frontmatter, fmErr := ExtractFrontmatter(ctx, content)
		if fmErr != nil {
			glog.V(3).Infof("FindTaskFilePath: skip %s (frontmatter error: %v)", relPath, fmErr)
			continue
		}
		var fm struct {
			TaskIdentifier string `yaml:"task_identifier"`
		}
		if umErr := yaml.Unmarshal([]byte(frontmatter), &fm); umErr != nil {
			glog.V(3).Infof("FindTaskFilePath: skip %s (unmarshal error: %v)", relPath, umErr)
			continue
		}
		glog.V(3).
			Infof("FindTaskFilePath: file %s has task_identifier=%s", relPath, fm.TaskIdentifier)
		if lib.TaskIdentifier(fm.TaskIdentifier) == id {
			matchedRelPath = relPath
			glog.V(2).Infof("FindTaskFilePath: matched file %s for task %s", matchedRelPath, id)
			var existingFm lib.TaskFrontmatter
			if umErr := yaml.Unmarshal([]byte(frontmatter), &existingFm); umErr != nil {
				glog.V(3).
					Infof("FindTaskFilePath: could not unmarshal existing frontmatter for %s: %v", relPath, umErr)
			} else {
				existingFrontmatter = existingFm
			}
		}
	}
	return matchedRelPath, existingFrontmatter, nil
}

func (r *resultWriter) WriteResult(ctx context.Context, req lib.Task) error {
	glog.V(2).Infof("WriteResult: starting for task %s", req.TaskIdentifier)
	glog.V(3).Infof("WriteResult: scanning taskDir=%s", r.taskDir)

	matchedRelPath, existingFrontmatter, err := FindTaskFilePath(
		ctx,
		r.gitClient,
		r.taskDir,
		req.TaskIdentifier,
	)
	if err != nil {
		return errors.Wrapf(ctx, err, "find task file path failed")
	}

	if matchedRelPath == "" {
		glog.Warningf("task file not found for identifier %s, skipping", req.TaskIdentifier)
		metrics.ResultsWrittenTotal.WithLabelValues("not_found").Inc()
		return nil
	}

	merged := mergeFrontmatter(existingFrontmatter, req.Frontmatter)
	body := r.applyRetryCounter(merged, existingFrontmatter, string(req.Content))

	marshaledFrontmatter, err := yaml.Marshal(map[string]any(merged))
	if err != nil {
		return errors.Wrapf(ctx, err, "marshal frontmatter failed")
	}

	newContent := []byte(
		"---\n" + string(marshaledFrontmatter) + "---\n" + body,
	)
	absPath := filepath.Join(r.gitClient.Path(), matchedRelPath)
	glog.V(2).Infof("WriteResult: writing and pushing for task %s", req.TaskIdentifier)
	if err := r.gitClient.AtomicWriteAndCommitPush(
		ctx,
		absPath,
		newContent,
		fmt.Sprintf("[agent-task-controller] write result for task %s", req.TaskIdentifier),
	); err != nil {
		metrics.ResultsWrittenTotal.WithLabelValues("error").Inc()
		return errors.Wrapf(ctx, err, "atomic write and push failed")
	}

	glog.V(2).Infof("WriteResult: completed successfully for task %s", req.TaskIdentifier)
	metrics.ResultsWrittenTotal.WithLabelValues("success").Inc()
	return nil
}

func (r *resultWriter) applyRetryCounter(merged, existing lib.TaskFrontmatter, body string) string {
	if string(merged.Status()) == "completed" {
		return body
	}

	// Trigger-count cap enforcement runs unconditionally before any early
	// returns below: it is a derived invariant on the on-disk state that
	// must hold after every WriteResult. Placing it here also prevents the
	// spawn_notification short-circuit below from silently skipping
	// escalation on agent result writes that inherited spawn_notification
	// from a previous merge (observed live on dev 2026-04-24, task
	// ba1bad61: spawn-notification update set spawn_notification=true,
	// then the agent's result publish inherited it via mergeFrontmatter
	// and skipped the cap check, reverting phase: human_review to
	// ai_review). The triggerCount > 0 guard prevents degenerate
	// escalation of brand-new tasks where trigger_count is absent.
	triggerCount := merged.TriggerCount()
	body = r.applyTriggerCap(merged, existing, triggerCount, body)

	// human_review assignee-clear guard runs BEFORE the spawn_notification
	// early return below. On a pr-reviewer agent's first post-spawn write, the merged
	// frontmatter carries spawn_notification: true (inherited from the executor's
	// spawn-time UpdateFrontmatterCommand) AND incoming phase: human_review (from
	// Result.NextPhase via resolveNextPhase). If this guard were below the
	// spawn_notification early return, clearAssignee would never fire and the
	// operator inbox filter (assignee == "") would miss the task. Same bug class
	// as prompt 075 (2026-04-24 applyTriggerCap precedent, task ba1bad61); spec 041
	// fixes the 2026-05-25 prod incident (task bborbe-agent #3).
	if phase, ok := merged["phase"].(string); ok && phase == "human_review" {
		clearAssignee(merged)
	}

	if merged.SpawnNotification() {
		delete(merged, "spawn_notification")
		return body
	}

	// retry_count is authoritative in the task file — the executor bumped it
	// at spawn time (spec 011). The writer only applies escalation.
	retryCount := merged.RetryCount()
	body = r.applyRetryCap(merged, existing, retryCount, body)

	return body
}

// applyTriggerCap enforces the trigger-count cap. The triggerCount > 0 guard prevents
// degenerate escalation of brand-new tasks where trigger_count is absent. When the task
// is already parked (section present), existing.Phase() restores the on-disk lifecycle
// phase to prevent stale-result phase clobber (cap stickiness).
func (r *resultWriter) applyTriggerCap(
	merged, existing lib.TaskFrontmatter,
	triggerCount int,
	body string,
) string {
	if triggerCount == 0 || triggerCount < merged.MaxTriggers() {
		return body
	}
	agentName := clearAssignee(merged)
	if containsEscalationSection(body, "## Trigger Cap Escalation") {
		restoreExistingPhase(existing, merged)
		return body
	}
	return body + r.triggerEscalationSection(triggerCount, agentName, merged)
}

// applyRetryCap enforces the retry-count cap. When the task is already parked (section
// present), existing.Phase() restores the on-disk lifecycle phase (cap stickiness).
func (r *resultWriter) applyRetryCap(
	merged, existing lib.TaskFrontmatter,
	retryCount int,
	body string,
) string {
	if retryCount < merged.MaxRetries() {
		return body
	}
	agentName := clearAssignee(merged)
	if containsEscalationSection(body, "## Retry Escalation") {
		restoreExistingPhase(existing, merged)
		return body
	}
	return body + r.escalationSection(retryCount, agentName)
}

// clearAssignee sets previous_assignee to the current assignee value (if non-empty),
// then clears assignee to "". Returns the captured name for use in escalation body text.
// This is the single chokepoint for all assignee-clear operations in the result writer.
func clearAssignee(merged lib.TaskFrontmatter) string {
	agentName := string(merged.Assignee())
	if agentName != "" {
		merged["previous_assignee"] = agentName
	}
	merged["assignee"] = ""
	return agentName
}

// restoreExistingPhase writes the on-disk phase back into merged when the existing
// frontmatter has a phase value. Used to enforce cap stickiness on repeated writes.
func restoreExistingPhase(existing, merged lib.TaskFrontmatter) {
	if p := existing.Phase(); p != nil {
		merged["phase"] = string(*p)
	}
}

func (r *resultWriter) escalationSection(retryCount int, agentName string) string {
	ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf(
		"\n## Retry Escalation\n\n- **Timestamp:** %s\n- **Attempts:** %d\n- **Assignee:** %s\n- **Last error:** see agent output above\n",
		ts,
		retryCount,
		agentName,
	)
}

func (r *resultWriter) triggerEscalationSection(
	triggerCount int,
	agentName string,
	merged lib.TaskFrontmatter,
) string {
	ts := r.currentDateTime.Now().UTC().Format(time.RFC3339)
	return fmt.Sprintf(
		"\n## Trigger Cap Escalation\n\n- **Timestamp:** %s\n- **Trigger count:** %d\n- **Max triggers:** %d\n- **Assignee:** %s\n- **Last agent output:** see `## Result` above\n",
		ts,
		triggerCount,
		merged.MaxTriggers(),
		agentName,
	)
}

// containsEscalationSection reports whether body already has the given
// escalation header on its own line. Used to prevent duplicate escalation
// sections when WriteResult runs multiple times on a task that is already
// at cap (e.g. agent publishes another result while the task sits in
// phase: human_review).
func containsEscalationSection(body, header string) bool {
	return strings.Contains(body, "\n"+header+"\n")
}

// mergeFrontmatter returns a new frontmatter map with all keys from existing,
// overridden by all keys from incoming. Neither input map is modified.
func mergeFrontmatter(existing, incoming lib.TaskFrontmatter) lib.TaskFrontmatter {
	merged := make(lib.TaskFrontmatter, len(existing)+len(incoming))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range incoming {
		merged[k] = v
	}
	return merged
}

// ExtractFrontmatter returns the YAML frontmatter string between the opening and
// closing "---" delimiters. Returns an error if delimiters are missing.
func ExtractFrontmatter(ctx context.Context, content []byte) (string, error) {
	s := string(content)
	const delim = "---"
	if !strings.HasPrefix(s, delim) {
		return "", errors.Errorf(ctx, "no frontmatter delimiter found")
	}
	rest := strings.TrimPrefix(s, delim)
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}
	before, _, found := strings.Cut(rest, "\n---")
	if !found {
		return "", errors.Errorf(ctx, "frontmatter not closed")
	}
	return before, nil
}

// ExtractBody returns the file body — the bytes after the closing "---\n" delimiter.
// Returns an empty string if the body is empty; error if delimiters are missing.
func ExtractBody(ctx context.Context, content []byte) (string, error) {
	s := string(content)
	const delim = "---"
	if !strings.HasPrefix(s, delim) {
		return "", errors.Errorf(ctx, "no frontmatter delimiter found")
	}
	rest := strings.TrimPrefix(s, delim)
	if strings.HasPrefix(rest, "\r\n") {
		rest = rest[2:]
	} else if strings.HasPrefix(rest, "\n") {
		rest = rest[1:]
	}
	_, after, found := strings.Cut(rest, "\n---\n")
	if !found {
		return "", errors.Errorf(ctx, "frontmatter not closed")
	}
	return after, nil
}
