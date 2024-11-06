// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"fmt"
	"net"
)

// GetIPv4CIDR returns an IPv4 CIDR
func GetIPv4CIDR(cidrs []string) (string, error) {
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			err := fmt.Errorf("unable to parse cidr: %s", cidr)
			return "", err
		}
		if ipNet.IP.To4() != nil {
			return cidr, nil
		}
	}
	return "", nil
}
