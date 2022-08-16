//go:build tools
// +build tools

/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package tools

import (
	_ "github.com/golang/mock/mockgen"
	_ "golang.org/x/tools/cmd/goimports"
)
