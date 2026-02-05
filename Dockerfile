# SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
#
# SPDX-License-Identifier: Apache-2.0

############# builder
FROM golang:1.26rc3 AS builder

WORKDIR /build

# Copy go mod and sum files
COPY go.mod go.sum ./
# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

COPY . .
ARG TARGETARCH
RUN make release GOARCH=$TARGETARCH

############# aws-custom-route-controller
FROM gcr.io/distroless/static-debian13:nonroot AS aws-custom-route-controller

COPY --from=builder /build/aws-custom-route-controller /aws-custom-route-controller
ENTRYPOINT ["/aws-custom-route-controller"]
