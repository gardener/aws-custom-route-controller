/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package updater

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
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
//go:generate mockgen -destination=mock_ec2.go -package=updater github.com/gardener/aws-custom-route-controller/pkg/updater EC2Routes
type EC2Routes interface {
	DescribeRouteTables(request *ec2.DescribeRouteTablesInput) (*ec2.DescribeRouteTablesOutput, error)
	CreateRoute(request *ec2.CreateRouteInput) (*ec2.CreateRouteOutput, error)
	DeleteRoute(request *ec2.DeleteRouteInput) (*ec2.DeleteRouteOutput, error)
}

func NewAWSEC2Routes(creds *Credentials, region string) (EC2Routes, error) {
	var (
		awsConfig = &aws.Config{
			Credentials: credentials.NewStaticCredentials(creds.AccessKeyID, creds.SecretAccessKey, ""),
		}
		config = &aws.Config{Region: aws.String(region)}
	)

	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, err
	}
	return ec2.New(s, config), nil
}

func ClusterTagKey(clusterID string) string {
	return TagNameKubernetesClusterPrefix + clusterID
}

func hasClusterTag(clusterID string, tags []*ec2.Tag) bool {
	clusterTagKey := ClusterTagKey(clusterID)
	for _, tag := range tags {
		tagKey := aws.StringValue(tag.Key)
		// For 1.6, we continue to recognize the legacy tags, for the 1.5 -> 1.6 upgrade
		// Note that we want to continue traversing tag list if we see a legacy tag with value != ClusterID
		if (tagKey == TagNameKubernetesClusterLegacy) && (aws.StringValue(tag.Value) == clusterID) {
			return true
		}
		if tagKey == clusterTagKey {
			return true
		}
	}
	return false
}

func getNameTagValue(tags []*ec2.Tag) string {
	for _, tag := range tags {
		if aws.StringValue(tag.Key) == "Name" {
			return aws.StringValue(tag.Value)
		}
	}
	return ""
}
