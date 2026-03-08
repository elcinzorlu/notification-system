package model_test

import (
	"testing"

	"github.com/elcinzorlu/notification-system/internal/notification/model"
	"github.com/stretchr/testify/assert"
)

func TestChannelIsValid(t *testing.T) {
	tests := []struct {
		name    string
		channel model.Channel
		valid   bool
	}{
		{"valid sms", model.ChannelSMS, true},
		{"valid email", model.ChannelEmail, true},
		{"valid push", model.ChannelPush, true},
		{"invalid channel", model.Channel("telegram"), false},
		{"empty channel", model.Channel(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.channel.IsValid())
		})
	}
}

func TestPriorityIsValid(t *testing.T) {
	tests := []struct {
		name     string
		priority model.Priority
		valid    bool
	}{
		{"valid high", model.PriorityHigh, true},
		{"valid normal", model.PriorityNormal, true},
		{"valid low", model.PriorityLow, true},
		{"invalid priority", model.Priority("critical"), false},
		{"empty priority", model.Priority(""), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.priority.IsValid())
		})
	}
}

func TestPriorityRabbitMQPriority(t *testing.T) {
	tests := []struct {
		name     string
		priority model.Priority
		expected uint8
	}{
		{"high", model.PriorityHigh, 10},
		{"normal", model.PriorityNormal, 5},
		{"low", model.PriorityLow, 1},
		{"unknown defaults to 5", model.Priority("unknown"), 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.priority.RabbitMQPriority())
		})
	}
}
