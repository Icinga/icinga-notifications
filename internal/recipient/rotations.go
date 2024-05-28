package recipient

import (
	"cmp"
	"slices"
	"time"
)

// rotationResolver stores all the rotations from a scheduled in a structured way that's suitable for evaluating them.
type rotationResolver struct {
	// sortedByPriority is ordered so that the elements at a smaller index have higher precedence.
	sortedByPriority []*rotationsWithPriority
}

// rotationsWithPriority stores the different versions of the rotations with the same priority within a single schedule.
type rotationsWithPriority struct {
	priority int32

	// sortedByHandoff contains the different version of a specific rotation sorted by their ActualHandoff time.
	// This allows using binary search to find the active version.
	sortedByHandoff []*Rotation
}

// update initializes the rotationResolver with the given rotations, resetting any previously existing state.
func (r *rotationResolver) update(rotations []*Rotation) {
	// Group sortedByHandoff by priority using a temporary map with the priority as key.
	prioMap := make(map[int32]*rotationsWithPriority)
	for _, rotation := range rotations {
		p := prioMap[rotation.Priority]
		if p == nil {
			p = &rotationsWithPriority{
				priority: rotation.Priority,
			}
			prioMap[rotation.Priority] = p
		}

		p.sortedByHandoff = append(p.sortedByHandoff, rotation)
	}

	// Copy it to a slice and sort it by priority so that these can easily be iterated by priority.
	rs := make([]*rotationsWithPriority, 0, len(prioMap))
	for _, rotation := range prioMap {
		rs = append(rs, rotation)
	}
	slices.SortFunc(rs, func(a, b *rotationsWithPriority) int {
		return cmp.Compare(a.priority, b.priority)
	})

	// Sort the different versions of the same rotation (i.e. same schedule and priority, differing in their handoff
	// time) by the handoff time so that the currently active version can be found with binary search.
	for _, rotation := range rs {
		slices.SortFunc(rotation.sortedByHandoff, func(a, b *Rotation) int {
			return a.ActualHandoff.Time().Compare(b.ActualHandoff.Time())
		})
	}

	r.sortedByPriority = rs
}

// getRotationsAt returns a slice of active rotations at the given time.
//
// For priority, there may be at most one active rotation version. This function return all rotation versions that
// are active at the given time t, ordered by priority (lower index has higher precedence).
func (r *rotationResolver) getRotationsAt(t time.Time) []*Rotation {
	rotations := make([]*Rotation, 0, len(r.sortedByPriority))

	for _, w := range r.sortedByPriority {
		i, found := slices.BinarySearchFunc(w.sortedByHandoff, t, func(rotation *Rotation, t time.Time) int {
			return rotation.ActualHandoff.Time().Compare(t)
		})

		// If a rotation version with sortedByHandoff[i].ActualHandoff == t is found, it just became valid and should be
		// used. Otherwise, BinarySearchFunc returns the first index i after t so that:
		//
		//   sortedByHandoff[i-1].ActualHandoff < t < sortedByHandoff[i].ActualHandoff
		//
		// Thus, the version at index i becomes active after t and the preceding one is still active.
		if !found {
			i--
		}

		// If all rotation versions have ActualHandoff > t, there is none that's currently active and i is negative.
		if i >= 0 {
			rotations = append(rotations, w.sortedByHandoff[i])
		}
	}

	return rotations
}

// getContactsAt evaluates the rotations by priority and returns all contacts active at the given time.
func (r *rotationResolver) getContactsAt(t time.Time) []*Contact {
	rotations := r.getRotationsAt(t)
	for _, rotation := range rotations {
		for _, member := range rotation.Members {
			for _, entry := range member.TimePeriodEntries {
				if entry.Contains(t) {
					var contacts []*Contact

					if member.Contact != nil {
						contacts = append(contacts, member.Contact)
					}

					if member.ContactGroup != nil {
						contacts = append(contacts, member.ContactGroup.Members...)
					}

					return contacts
				}
			}
		}
	}

	return nil
}
