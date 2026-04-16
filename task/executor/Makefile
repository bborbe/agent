include ../../Makefile.variables
include ../../Makefile.precommit
include ../../Makefile.docker
include ../../Makefile.env
include ../../common.env

SERVICE = agent-task-executor

.PHONY: generatek8s
generatek8s:
	go mod vendor
	bash hack/update-codegen.sh
	rm -rf vendor
