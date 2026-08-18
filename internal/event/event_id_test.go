package event

import (
	"testing"
	"time"

	"github.com/google/uuid"
	baseEv "github.com/icinga/icinga-go-library/notifications/event"
	"github.com/icinga/icinga-go-library/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnsureID(t *testing.T) {
	t.Parallel()

	t.Run("Deterministic For Identical Content", func(t *testing.T) {
		t.Parallel()

		newEvent := func() Event {
			return Event{
				Time:     time.Unix(1700000000, 0),
				SourceId: 1,
				Event: baseEv.Event{
					Name:    "dummy: random fortune",
					Message: "Something went somewhere very wrong.",
					Tags:    map[string]string{"host": "dummy"},
				},
			}
		}

		a := newEvent()
		b := newEvent()

		require.NoError(t, a.EnsureID())
		require.NoError(t, b.EnsureID())

		assert.NotEqual(t, types.UUID{}, a.ID, "EnsureID should generate a non-zero ID")
		assert.Equal(t, a.ID, b.ID, "identical event content should hash to the same ID")
	})

	t.Run("Noop When ID Already Set", func(t *testing.T) {
		t.Parallel()

		existing := types.UUID{UUID: uuid.New()}
		ev := Event{
			SourceId: 1,
			Event: baseEv.Event{
				ID:      existing,
				Name:    "dummy: random fortune",
				Message: "Something went somewhere very wrong.",
			},
		}

		require.NoError(t, ev.EnsureID())
		assert.Equal(t, existing, ev.ID, "EnsureID must not overwrite an already-set ID")
	})

	t.Run("Different Content Yields Different ID", func(t *testing.T) {
		t.Parallel()

		a := Event{
			SourceId: 1,
			Event: baseEv.Event{
				Name:    "dummy: random fortune",
				Message: "first message",
			},
		}
		b := Event{
			SourceId: 1,
			Event: baseEv.Event{
				Name:    "dummy: random fortune",
				Message: "second message",
			},
		}

		require.NoError(t, a.EnsureID())
		require.NoError(t, b.EnsureID())

		assert.NotEqual(t, a.ID, b.ID, "events differing in content must not hash to the same ID")
	})
}

func TestCreateEvent(t *testing.T) {
	t.Parallel()

	baseEvent := baseEv.Event{
		Name:    "dummy: random fortune",
		Message: "Something went somewhere very wrong.",
	}

	ev, err := CreateEvent(42, baseEvent)
	require.NoError(t, err)

	assert.EqualValues(t, 42, ev.SourceId)
	assert.Equal(t, baseEvent.Name, ev.Name)
	assert.Equal(t, baseEvent.Message, ev.Message)
	assert.NotEqual(t, types.UUID{}, ev.ID, "CreateEvent must ensure a non-zero ID")
	assert.False(t, ev.Time.IsZero(), "CreateEvent must set Time")
}
