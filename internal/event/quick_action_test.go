package event

import (
	"testing"

	"github.com/icinga/icinga-go-library/testutils"
)

func TestAction(t *testing.T) {
	t.Parallel()

	t.Run("UnmarshallJSON", func(t *testing.T) {
		t.Parallel()

		testdata := []testutils.TestCase[Action, string]{
			{Name: "Manage", Expected: ActionManage, Data: `"manage"`},
			{Name: "Unmanage", Expected: ActionUnmanage, Data: `"unmanage"`},
			{Name: "Subscribe", Expected: ActionSubscribe, Data: `"subscribe"`},
			{Name: "Unsubscribe", Expected: ActionUnsubscribe, Data: `"unsubscribe"`},
			{Name: "Invalid Action", Expected: ActionInvalid, Data: `"invalid"`, Error: testutils.ErrorContains(`invalid quick action: "invalid"`)},
			{Name: "Invalid JSON", Expected: ActionInvalid, Data: `invalid`, Error: testutils.ErrorContains(`invalid character 'i' looking for beginning of value`)},
		}

		for _, tt := range testdata {
			t.Run(tt.Name, tt.F(func(s string) (Action, error) {
				var a Action
				err := a.UnmarshalJSON([]byte(s))
				return a, err
			}))
		}
	})
}
