# Copyright 2026 Authors of terminus
# SPDX-License-Identifier: Apache-2.0

ARG BASE_IMAGE=docker.m.daocloud.io/library/alpine
FROM ${BASE_IMAGE}

COPY ./terminus-scheduler-linux   /usr/bin/terminus-scheduler
CMD ["/usr/bin/terminus-scheduler"]