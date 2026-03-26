ARG DOCKER_REGISTRY=docker.quant.benjamin-borbe.de:443
FROM ${DOCKER_REGISTRY}/golang:1.26.1 AS build
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
COPY . /workspace
WORKDIR /workspace
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=vendor -ldflags "-s" -a -installsuffix cgo -o /main
CMD ["/bin/bash"]

FROM ${DOCKER_REGISTRY}/alpine:3.23
RUN apk --no-cache add \
	ca-certificates \
	git \
	gnupg \
	openssh-client \
	tzdata \
	&& rm -rf /var/cache/apk/*
ARG BUILD_GIT_COMMIT=none
ARG BUILD_DATE=unknown
COPY --from=build /main /main
ENV BUILD_GIT_COMMIT=${BUILD_GIT_COMMIT}
ENV BUILD_DATE=${BUILD_DATE}
ENTRYPOINT ["/main"]
