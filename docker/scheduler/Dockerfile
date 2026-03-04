# Copyright 2026 Authors of terminus
# SPDX-License-Identifier: Apache-2.0

ARG BASE_IMAGE=docker.m.daocloud.io/library/alpine
ARG BUILD_IMAGE=docker.m.daocloud.io/library/golang:1.25-alpine

FROM ${BUILD_IMAGE} AS builder
WORKDIR /app
COPY . .
RUN apk add --no-cache  make 
RUN make build-scheduler

FROM ${BASE_IMAGE}

COPY --from=builder /app/bin/terminus-scheduler   /usr/bin/terminus-scheduler
CMD ["/usr/bin/terminus-scheduler"]