package icinga2

import (
	"github.com/stretchr/testify/assert"
	"net/http"
	"testing"
)

func TestCheckHTTPResponseStatusCode(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		status     string
		wantErr    assert.ErrorAssertionFunc
	}{
		{"http-200", http.StatusOK, "200 OK", assert.NoError},
		{"http-299", 299, "299 ???", assert.NoError},
		{"http-204", http.StatusNoContent, "204 No Content", assert.NoError},
		{"http-401", http.StatusUnauthorized, "401 Unauthorized", assert.Error},
		{"http-500", http.StatusInternalServerError, "500 Internal Server Error", assert.Error},
		{"http-900", 900, "900 ???", assert.Error},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.wantErr(t, checkHTTPResponseStatusCode(tt.statusCode, tt.status))
		})
	}
}
