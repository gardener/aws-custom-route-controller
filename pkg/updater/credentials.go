/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package updater

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	// AccessKeyID is a constant for the key in a cloud provider secret and backup secret that holds the AWS access key id.
	AccessKeyID = "accessKeyID"
	// SecretAccessKey is a constant for the key in a cloud provider secret and backup secret that holds the AWS secret access key.
	SecretAccessKey = "secretAccessKey"
	// InClusterConfig is a special name for the kubeconfig to use in-cluster client
	InClusterConfig = "inClusterConfig"
)

type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}

func LoadCredentials(controlKubeconfig, namespace, secretName string) (*Credentials, error) {
	var err error
	var config *rest.Config
	if controlKubeconfig == InClusterConfig || controlKubeconfig == "" {
		config, err = rest.InClusterConfig()
	} else {
		config, err = clientcmd.BuildConfigFromFlags("", controlKubeconfig)
	}
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	secret, err := clientset.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return extractCredentials(secret)
}

func extractCredentials(secret *corev1.Secret) (*Credentials, error) {
	if secret.Data == nil {
		return nil, fmt.Errorf("secret does not contain any data")
	}

	accessKeyID, err := getSecretDataValue(secret, AccessKeyID, nil, true)
	if err != nil {
		return nil, err
	}

	secretAccessKey, err := getSecretDataValue(secret, SecretAccessKey, nil, true)
	if err != nil {
		return nil, err
	}

	return &Credentials{
		AccessKeyID:     string(accessKeyID),
		SecretAccessKey: string(secretAccessKey),
	}, nil
}

func getSecretDataValue(secret *corev1.Secret, key string, altKey *string, required bool) ([]byte, error) {
	if value, ok := secret.Data[key]; ok {
		return value, nil
	}
	if altKey != nil {
		if value, ok := secret.Data[*altKey]; ok {
			return value, nil
		}
	}
	if required {
		if altKey != nil {
			return nil, fmt.Errorf("missing %q (or %q) field in secret", key, *altKey)
		}
		return nil, fmt.Errorf("missing %q field in secret", key)
	}
	return nil, nil
}
