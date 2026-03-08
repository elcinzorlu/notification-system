package service_test

import (
	"testing"

	"github.com/elcinzorlu/notification-system/internal/notification/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplateString(t *testing.T) {
	tests := []struct {
		name     string
		template string
		vars     map[string]string
		expected string
		wantErr  bool
	}{
		{
			name:     "simple substitution",
			template: "Hello {{.Name}}, your order {{.OrderID}} is ready!",
			vars:     map[string]string{"Name": "Elçin", "OrderID": "12345"},
			expected: "Hello Elçin, your order 12345 is ready!",
			wantErr:  false,
		},
		{
			name:     "no variables",
			template: "Hello World!",
			vars:     map[string]string{},
			expected: "Hello World!",
			wantErr:  false,
		},
		{
			name:     "invalid template",
			template: "Hello {{.Name",
			vars:     map[string]string{"Name": "Test"},
			expected: "",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.RenderTemplateString(tt.template, tt.vars)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
