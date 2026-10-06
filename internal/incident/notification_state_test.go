package incident

import (
	"database/sql/driver"
	"testing"

	"github.com/icinga/icinga-go-library/testutils"
)

func TestNotificationState(t *testing.T) {
	t.Parallel()

	t.Run("MarshalJSON", func(t *testing.T) {
		t.Parallel()

		testData := []testutils.TestCase[string, NotificationState]{
			{Name: "Null", Expected: "null", Data: NotificationStateNull, Error: nil},
			{Name: "Suppressed", Expected: `"suppressed"`, Data: NotificationStateSuppressed, Error: nil},
			{Name: "Pending", Expected: `"pending"`, Data: NotificationStatePending, Error: nil},
			{Name: "Sent", Expected: `"sent"`, Data: NotificationStateSent, Error: nil},
			{Name: "Failed", Expected: `"failed"`, Data: NotificationStateFailed, Error: nil},
		}

		for _, tt := range testData {
			t.Run(tt.Name, tt.F(func(n NotificationState) (string, error) {
				data, err := n.MarshalJSON()
				return string(data), err
			}))
		}
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		t.Parallel()

		testData := []testutils.TestCase[NotificationState, string]{
			{Name: "Null", Expected: NotificationStateNull, Data: `null`, Error: nil},
			{Name: "Suppressed", Expected: NotificationStateSuppressed, Data: `"suppressed"`, Error: nil},
			{Name: "Pending", Expected: NotificationStatePending, Data: `"pending"`, Error: nil},
			{Name: "Sent", Expected: NotificationStateSent, Data: `"sent"`, Error: nil},
			{Name: "Failed", Expected: NotificationStateFailed, Data: `"failed"`, Error: nil},
			{Name: "Invalid", Expected: NotificationStateNull, Data: `"invalid"`, Error: testutils.ErrorContains(`unknown notification state "invalid"`)},
			{Name: "Not a string", Expected: NotificationStateNull, Data: `42`, Error: testutils.ErrorContains("cannot unmarshal")},
		}

		for _, tt := range testData {
			t.Run(tt.Name, tt.F(func(input string) (NotificationState, error) {
				var n NotificationState
				return n, n.UnmarshalJSON([]byte(input))
			}))
		}
	})

	t.Run("Scan", func(t *testing.T) {
		t.Parallel()

		testData := []testutils.TestCase[NotificationState, any]{
			{Name: "Null", Expected: NotificationStateNull, Data: nil, Error: nil},
			{Name: "Suppressed", Expected: NotificationStateSuppressed, Data: `suppressed`, Error: nil},
			{Name: "Pending", Expected: NotificationStatePending, Data: `pending`, Error: nil},
			{Name: "Sent", Expected: NotificationStateSent, Data: `sent`, Error: nil},
			{Name: "Failed", Expected: NotificationStateFailed, Data: `failed`, Error: nil},
			{Name: "Invalid", Expected: NotificationStateNull, Data: `invalid`, Error: testutils.ErrorContains(`unknown notification state "invalid"`)},
			{Name: "Not a string", Expected: NotificationStateNull, Data: 42, Error: testutils.ErrorContains(`unable to scan type int into NotificationState`)},
		}

		for _, tt := range testData {
			t.Run(tt.Name, tt.F(func(input any) (NotificationState, error) {
				var n NotificationState
				return n, n.Scan(input)
			}))
		}
	})

	t.Run("Value", func(t *testing.T) {
		t.Parallel()

		testData := []testutils.TestCase[driver.Value, NotificationState]{
			{Name: "Null", Expected: nil, Data: NotificationStateNull, Error: nil},
			{Name: "Suppressed", Expected: `suppressed`, Data: NotificationStateSuppressed, Error: nil},
			{Name: "Pending", Expected: `pending`, Data: NotificationStatePending, Error: nil},
			{Name: "Sent", Expected: `sent`, Data: NotificationStateSent, Error: nil},
			{Name: "Failed", Expected: `failed`, Data: NotificationStateFailed, Error: nil},
		}

		for _, tt := range testData {
			t.Run(tt.Name, tt.F(func(n NotificationState) (driver.Value, error) { return n.Value() }))
		}
	})
}
