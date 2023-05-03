# SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

############# builder
FROM golang:1.20.4 AS builder

WORKDIR /build
COPY . .
ARG TARGETARCH
RUN make release GOARCH=$TARGETARCH

############# aws-custom-route-controller
FROM gcr.io/distroless/static-debian11:nonroot AS aws-custom-route-controller

COPY --from=builder /build/aws-custom-route-controller /aws-custom-route-controller
ENTRYPOINT ["/aws-custom-route-controller"]
