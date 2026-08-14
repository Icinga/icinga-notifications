package event

import (
	"database/sql/driver"
	"fmt"
	"testing"

	"github.com/icinga/icinga-go-library/testutils"
)

func TestEnvelope(t *testing.T) {
	t.Parallel()

	t.Run("Scan", func(t *testing.T) {
		t.Parallel()

		testData := []testutils.TestCase[Envelope, any]{
			{Name: "Nil", Expected: Envelope{}, Data: nil},
			{Name: "Empty", Expected: Envelope{}, Data: "{}"},
			{Name: "Valid String", Expected: Envelope{Version: EnvelopeEventVersion, Format: EnvelopeFmtEvent}, Data: `{"version":1,"format":"event"}`},
			{Name: "Valid Bytes", Expected: Envelope{Version: EnvelopeEventVersion, Format: EnvelopeFmtEvent}, Data: []byte(`{"version":1,"format":"event"}`)},
			{Name: "Invalid Format", Expected: Envelope{}, Data: `{"version":1,"format":"invalid"}`, Error: testutils.ErrorContains(`unknown envelope format "invalid"`)},
			{Name: "Invalid Json", Expected: Envelope{}, Data: 123, Error: testutils.ErrorContains(`cannot scan envelope from int`)},
			{Name: "Invalid String", Expected: Envelope{}, Data: `{"version":1,"format":"event`, Error: testutils.ErrorContains(`unexpected end of JSON input`)},
			{Name: "Invalid Bytes", Expected: Envelope{}, Data: []byte(`{"version":1,"format":"event`), Error: testutils.ErrorContains(`unexpected end of JSON input`)},
		}

		for _, tt := range testData {
			t.Run(tt.Name, tt.F(func(input any) (Envelope, error) {
				var e Envelope
				return e, e.Scan(input)
			}))
		}
	})

	t.Run("Value", func(t *testing.T) {
		t.Parallel()

		testdata := []testutils.TestCase[driver.Value, Envelope]{
			{Name: "Valid", Expected: `{"version":1,"format":"event"}`, Data: Envelope{Version: 1, Format: EnvelopeFmtEvent}},
			{Name: "Empty", Expected: `{"version":0,"format":"none"}`, Data: Envelope{}},
			{
				Name:     "Invalid Payload",
				Expected: nil,
				Data:     Envelope{Version: 1, Format: EnvelopeFmtEvent, Payload: []byte(`{"invalid_json`)},
				Error:    testutils.ErrorContains(`unexpected end of JSON input`),
			},
		}

		for _, tt := range testdata {
			t.Run(tt.Name, tt.F(func(e Envelope) (driver.Value, error) {
				b, err := e.Value()
				if err != nil {
					return nil, err
				}
				return fmt.Sprintf("%s", b), nil
			}))
		}
	})

	t.Run("Envelope Format", func(t *testing.T) {
		t.Parallel()

		t.Run("MarshalJSON", func(t *testing.T) {
			t.Parallel()

			testdata := []testutils.TestCase[string, EnvelopeFmt]{
				{Name: "Valid", Expected: `"event"`, Data: EnvelopeFmtEvent},
				{Name: "None", Expected: `"none"`, Data: EnvelopeFmt(0)},
			}

			for _, tt := range testdata {
				t.Run(tt.Name, tt.F(func(e EnvelopeFmt) (string, error) {
					b, err := e.MarshalJSON()
					return string(b), err
				}))
			}
		})

		t.Run("UnmarshalJSON", func(t *testing.T) {
			t.Parallel()

			testdata := []testutils.TestCase[EnvelopeFmt, string]{
				{Name: "Valid", Expected: EnvelopeFmtEvent, Data: `"event"`},
				{Name: "Invalid Format", Expected: EnvelopeFmtNone, Data: `"invalid"`, Error: testutils.ErrorContains(`unknown envelope format "invalid"`)},
				{Name: "Invalid JSON", Expected: EnvelopeFmtNone, Data: `invalid`, Error: testutils.ErrorContains(`invalid character 'i' looking for beginning of value`)},
			}

			for _, tt := range testdata {
				t.Run(tt.Name, tt.F(func(s string) (EnvelopeFmt, error) {
					var e EnvelopeFmt
					err := e.UnmarshalJSON([]byte(s))
					return e, err
				}))
			}
		})
	})
}
