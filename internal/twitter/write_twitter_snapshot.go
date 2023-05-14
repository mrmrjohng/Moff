package twitter

import (
	"moff.io/moff-social/internal/database"
	"moff.io/moff-social/pkg/errors"
)

func WriteTwitterSnapshot(spaceID string) error {
	// 查询快照
	snapshots, err := database.TwitterSpaceSnapshots{}.SelectOne(spaceID)
	if err != nil {
		return err
	}
	// 查询owner
	owners, err := database.TwitterSpaceOwnerships{}.SelectSpaceOwners(spaceID)
	if err != nil {
		return err
	}
	if len(owners) == 0 {
		return errors.New("no owner found")
	}

	monitor := SpaceMonitor{
		snapshot: snapshots,
	}
	participants, err := monitor.loadSpaceParticipants(spaceID)
	if err != nil {
		return err
	}
	monitor.spaceParticipants = participants

	monitor.finalize()
	return nil
}
