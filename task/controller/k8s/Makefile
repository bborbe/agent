include ../../../Makefile.variables
include ../../../Makefile.env
include ../../../Makefile.k8s

# VAULTS tuple: '<vault-name>=<gateway-secret-teamvault-id>'
# Stage-split: tvids differ per environment. Pattern mirrors trading/mt5/connector/k8s/Makefile.
ifeq ($(BRANCH),prod)
	VAULTS ?= \
		'openclaw=3OlKKw' \
		'personal=dwkkgw'
else ifeq ($(BRANCH),dev)
	VAULTS ?= \
		'openclaw=YLbzyL' \
		'personal=9L6eMO'
else
	$(error apply requires BRANCH=dev or BRANCH=prod)
endif

apply:
	@for i in $(VAULTS); do \
		export VAULT="$$(echo $$i | cut -d'=' -f1)"; \
		export GATEWAY_SECRET_TVID="$$(echo $$i | cut -d'=' -f2)"; \
		echo "applying for vault $${VAULT}"; \
		$(MAKE) applyOne; \
	done

applyOne:
	@if [ "${BRANCH}" = "prod" ]; then \
		bash -c 'source ${ROOTDIR}/prod.env && source ${ROOTDIR}/common.env && find . -name "*.yaml" | while read -r file; do cat $${file} | teamvault-config-parser -teamvault-config="${TEAMVAULT}" -logtostderr -v=0 | kubectlquant apply -f -; done'; \
	elif [ "${BRANCH}" = "dev" ]; then \
		bash -c 'source ${ROOTDIR}/dev.env && source ${ROOTDIR}/common.env && find . -name "*.yaml" | while read -r file; do cat $${file} | teamvault-config-parser -teamvault-config="${TEAMVAULT}" -logtostderr -v=0 | kubectlquant apply -f -; done'; \
	else \
		echo 'apply only allowed in prod or dev branch!' >&2; \
		exit 1; \
	fi
