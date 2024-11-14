/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package updater

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	v2config "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// TagNameKubernetesClusterPrefix is the tag name we use to differentiate multiple
// logically independent clusters running in the same AZ.
// The tag key = TagNameKubernetesClusterPrefix + clusterID
// The tag value is an ownership value
const TagNameKubernetesClusterPrefix = "kubernetes.io/cluster/"

// TagNameKubernetesClusterLegacy is the legacy tag name we use to differentiate multiple
// logically independent clusters running in the same AZ.  The problem with it was that it
// did not allow shared resources.
const TagNameKubernetesClusterLegacy = "KubernetesCluster"

// EC2Routes is an abstraction over AWS EC2, to allow mocking/other implementations
//
//go:generate ${MOCKGEN} -destination=mock_ec2.go -package=updater github.com/gardener/aws-custom-route-controller/pkg/updater EC2Routes
type EC2Routes interface {
	DescribeRouteTables(ctx context.Context, params *ec2.DescribeRouteTablesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeRouteTablesOutput, error)
	CreateRoute(ctx context.Context, params *ec2.CreateRouteInput, optFns ...func(*ec2.Options)) (*ec2.CreateRouteOutput, error)
	DeleteRoute(ctx context.Context, params *ec2.DeleteRouteInput, optFns ...func(*ec2.Options)) (*ec2.DeleteRouteOutput, error)
}

func NewAWSEC2Routes(creds *Credentials, region string) (EC2Routes, error) {
	cfg, err := v2config.LoadDefaultConfig(
		context.TODO(),
		v2config.WithRegion(region),
		v2config.WithCredentialsProvider(aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, ""))),
	)
	if err != nil {
		return nil, err
	}

	return ec2.NewFromConfig(cfg), nil
}

func ClusterTagKey(clusterID string) string {
	return TagNameKubernetesClusterPrefix + clusterID
}

func hasClusterTag(clusterID string, tags []ec2types.Tag) bool {
	clusterTagKey := ClusterTagKey(clusterID)
	for _, tag := range tags {
		if tag.Key == nil || tag.Value == nil {
			continue
		}
		// For 1.6, we continue to recognize the legacy tags, for the 1.5 -> 1.6 upgrade
		// Note that we want to continue traversing tag list if we see a legacy tag with value != ClusterID
		if (*tag.Key == TagNameKubernetesClusterLegacy) && (*tag.Value == clusterID) {
			return true
		}
		if *tag.Key == clusterTagKey {
			return true
		}
	}
	return false
}

func getNameTagValue(tags []ec2types.Tag) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == "Name" {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}
