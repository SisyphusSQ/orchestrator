package inst

import (
	"context"

	"github.com/openark/orchestrator/internal/golib/log"
	"github.com/openark/orchestrator/internal/repository/topology"
)

// PopulateGroupReplicationInformation obtains member information for one replication group.
func PopulateGroupReplicationInformation(instance *Instance, db *topology.Client) error {
	members, rowErrors, supported, err := db.ReadGroupReplicationMembers(context.Background())
	if err != nil {
		return log.Errorf("There was an error trying to check group replication information for instance "+
			"%+v: %+v", instance.Key, err)
	}
	if !supported {
		return nil
	}
	for _, rowErr := range rowErrors {
		log.Errorf("Unable to scan row group replication information while processing %+v, skipping the row and continuing: %+v", instance.Key, rowErr)
	}
	foundGroupPrimary := false
	// Loop over the query results and populate GR instance attributes from the row that matches the instance being
	// probed. In addition, figure out the group primary and also add it as attribute of the instance.
	for _, member := range members {
		// ToDo: add support for multi primary groups.
		if !member.SinglePrimaryGroup {
			log.Debugf("This host seems to belong to a multi-primary replication group, which we don't " +
				"support")
			break
		}
		groupMemberKey, err := NewResolveInstanceKey(member.Host, int(member.Port))
		if err != nil {
			log.Errorf("Unable to resolve instance for group member %v:%v", member.Host, member.Port)
			continue
		}
		// Set the replication group primary from what we find in performance_schema.replication_group_members for
		// the instance being discovered.
		if member.Role == GroupReplicationMemberRolePrimary && groupMemberKey != nil {
			instance.ReplicationGroupPrimaryInstanceKey = *groupMemberKey
			foundGroupPrimary = true
		}
		if member.UUID == instance.ServerUUID {
			instance.ReplicationGroupName = member.GroupName
			instance.ReplicationGroupIsSinglePrimary = member.SinglePrimaryGroup
			instance.ReplicationGroupMemberRole = member.Role
			instance.ReplicationGroupMemberState = member.State
		} else {
			instance.AddGroupMemberKey(groupMemberKey) // This helps us keep info on all members of the same group as the instance
		}
	}
	// If we did not manage to find the primary of the group in performance_schema.replication_group_members, we are
	// likely to have been expelled from the group. Still, try to find out the primary of the group and set it for the
	// instance being discovered, so that it is identified as part of the same cluster
	if !foundGroupPrimary {
		err = ReadReplicationGroupPrimary(instance)
		if err != nil {
			return log.Errorf("Unable to find the group primary of instance %+v even though it seems to be "+
				"part of a replication group", instance.Key)
		}
	}
	return nil
}
