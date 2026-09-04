package metacontainer

import "fmt"

// ErrNotFound is returned by Container.Get when the blob key does not exist.
type ErrNotFound struct{ Key string }

func (e *ErrNotFound) Error() string { return fmt.Sprintf("blob not found: %s", e.Key) }

// ErrWorkspaceNotFound is returned when a cluster's workspace.json does not exist
// in the meta container.
type ErrWorkspaceNotFound struct {
	ClusterName    string
	SubscriptionID string
}

func (e *ErrWorkspaceNotFound) Error() string {
	return fmt.Sprintf(
		"no workspace found for cluster %q in subscription %s.\n\n"+
			"Possible causes:\n"+
			"  1. The cluster was destroyed previously.\n"+
			"  2. The cluster was created from a different Azure subscription.\n"+
			"  3. The cluster name is misspelled.\n\n"+
			"To see clusters known in this subscription:\n"+
			"  wasctl workspace list\n\n"+
			"If you know the cluster exists but the workspace is gone, see:\n"+
			"  docs/Operations.md (Recovery tips)",
		e.ClusterName, e.SubscriptionID,
	)
}
