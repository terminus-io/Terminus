# Copyright 2026 Authors of terminus
# SPDX-License-Identifier: Apache-2.0

ARG BASE_IMAGE=docker.m.daocloud.io/library/alpine

FROM ${BASE_IMAGE}
ARG GIT_COMMIT_VERSION
ENV GIT_COMMIT_VERSION=${GIT_COMMIT_VERSION}
ARG GIT_COMMIT_TIME
ENV GIT_COMMIT_TIME=${GIT_COMMIT_TIME}
ARG VERSION
ENV VERSION=${VERSION}

COPY terminus-scheduler-linux   /usr/bin/terminus-scheduler
CMD ["/usr/bin/terminus-scheduler"]