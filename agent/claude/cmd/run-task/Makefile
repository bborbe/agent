include ../../../../Makefile.variables
include ../../../../common.env

TASK_FILE ?= dummy-task.md
BRANCH := dev
CLAUDE_CONFIG_DIR ?= $(HOME)/.claude-agent
AGENT_DIR ?= $(shell cd ../.. && pwd)/agent
MODEL ?= sonnet
ALLOWED_TOOLS ?= Grep,Read

generate-dummy-task:
	@echo '---' > $(TASK_FILE)
	@echo 'status: in_progress' >> $(TASK_FILE)
	@echo 'phase: planning' >> $(TASK_FILE)
	@echo 'title: Dummy Claude Task' >> $(TASK_FILE)
	@echo 'assignee: claude-agent' >> $(TASK_FILE)
	@echo '---' >> $(TASK_FILE)
	@echo '' >> $(TASK_FILE)
	@echo 'Say hello and report done.' >> $(TASK_FILE)
	@echo "generated $(TASK_FILE)"

run-dummy-task: $(TASK_FILE)
	@go run -mod=mod . \
		-task-file=$(TASK_FILE) \
		-branch=$(BRANCH) \
		-agent-dir=$(AGENT_DIR) \
		-claude-config-dir=$(CLAUDE_CONFIG_DIR) \
		-model=$(MODEL) \
		-allowed-tools=$(ALLOWED_TOOLS) \
		-v=2
