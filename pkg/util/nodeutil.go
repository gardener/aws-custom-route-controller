/*
 * SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Gardener contributors
 *
 * SPDX-License-Identifier: Apache-2.0
 */

package util

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetNodeCondition extracts the provided condition from the given node status and returns that.
// Returns nil and -1 if the condition is not present, and the index of the located condition.
func GetNodeCondition(status *corev1.NodeStatus, conditionType corev1.NodeConditionType) (int, *corev1.NodeCondition) {
	if status == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}

// SetNodeCondition updates the node with the provided condition
func SetNodeCondition(ctx context.Context, c client.Client, nodeName types.NodeName, condition corev1.NodeCondition) error {

	// Get the current node - this will be refetched in each retry attempt
	node := &corev1.Node{}
	if err := c.Get(ctx, types.NamespacedName{Name: string(nodeName)}, node); err != nil {
		return err
	}

	// Clone the node for patching
	oldNode := node.DeepCopy()

	// Update or add the condition
	_, oldCondition := GetNodeCondition(&node.Status, condition.Type)

	// Check if the condition is already set to avoid unnecessary updates
	if oldCondition != nil &&
		oldCondition.Status == condition.Status &&
		oldCondition.Reason == condition.Reason &&
		oldCondition.Message == condition.Message {
		// Condition already matches, no update needed
		return nil
	}

	if oldCondition == nil {
		// Condition doesn't exist, add it
		node.Status.Conditions = append(node.Status.Conditions, condition)
	} else {
		// Condition exists, update it
		for i := range node.Status.Conditions {
			if node.Status.Conditions[i].Type == condition.Type {
				node.Status.Conditions[i] = condition
				break
			}
		}
	}

	// Create and apply the patch
	oldData, err := json.Marshal(oldNode)
	if err != nil {
		return fmt.Errorf("failed to marshal old node: %w", err)
	}

	newData, err := json.Marshal(node)
	if err != nil {
		return fmt.Errorf("failed to marshal new node: %w", err)
	}

	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, corev1.Node{})
	if err != nil {
		return fmt.Errorf("failed to create patch: %w", err)
	}

	// Apply the patch to node status
	if err := c.Status().Patch(ctx, node, client.RawPatch(types.StrategicMergePatchType, patchBytes)); err != nil {
		return fmt.Errorf("failed to patch node status: %w", err)
	}

	return nil
}
