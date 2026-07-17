package acp

import "encoding/json"

// DefaultNotificationReceiver can be embedded by clients that only care
// about a subset of ACP notifications.
type DefaultNotificationReceiver struct{}

func (DefaultNotificationReceiver) SessionUpdate(notification *SessionNotification) {}

func (DefaultNotificationReceiver) UserMessageChunk(sessionID SessionID, content ContentBlock, messageID *MessageID) {
}

func (DefaultNotificationReceiver) AgentMessageChunk(sessionID SessionID, content ContentBlock, messageID *MessageID) {
}

func (DefaultNotificationReceiver) AgentThoughtChunk(sessionID SessionID, thought string, content ContentBlock) {
}

func (DefaultNotificationReceiver) ToolCall(sessionID SessionID, toolCall *ToolCall) {}

func (DefaultNotificationReceiver) ToolCallUpdate(sessionID SessionID, update *ToolCallUpdate) {}

func (DefaultNotificationReceiver) Plan(sessionID SessionID, plan *Plan) {}

func (DefaultNotificationReceiver) AvailableCommandsUpdate(sessionID SessionID, update *AvailableCommandsUpdate) {
}

func (DefaultNotificationReceiver) CurrentModeUpdate(sessionID SessionID, update *CurrentModeUpdate) {
}

func (DefaultNotificationReceiver) ConfigOptionUpdate(sessionID SessionID, update *ConfigOptionUpdate) {
}

func (DefaultNotificationReceiver) SessionInfoUpdate(sessionID SessionID, update *SessionInfoUpdate) {
}

func (DefaultNotificationReceiver) UsageUpdate(sessionID SessionID, update *UsageUpdate) {}

func (DefaultNotificationReceiver) UnknownSessionUpdate(notification *SessionNotification) {}

func (DefaultNotificationReceiver) UnknownNotification(method string, params json.RawMessage) {}

func (DefaultNotificationReceiver) InvalidNotification(method string, params json.RawMessage, err error) {
}

// NewNotificationHandler converts a typed receiver into the raw callback shape
// accepted by Client.OnNotification.
func NewNotificationHandler(receiver NotificationReceiver) NotificationHandler {
	return func(method string, params json.RawMessage) {
		DispatchNotification(receiver, method, params)
	}
}

// DispatchNotification parses an ACP JSON-RPC notification and calls the
// matching typed receiver method.
func DispatchNotification(receiver NotificationReceiver, method string, params json.RawMessage) {
	if receiver == nil {
		return
	}
	switch method {
	case "session/update":
		var notification SessionNotification
		if err := json.Unmarshal(params, &notification); err != nil {
			receiver.InvalidNotification(method, params, err)
			return
		}
		receiver.SessionUpdate(&notification)
		dispatchSessionUpdate(receiver, &notification)
	default:
		receiver.UnknownNotification(method, params)
	}
}

func dispatchSessionUpdate(receiver NotificationReceiver, notification *SessionNotification) {
	update := notification.Update
	switch update.SessionUpdate {
	case "user_message_chunk":
		receiver.UserMessageChunk(notification.SessionID, update.Content, update.MessageID)
	case "agent_message_chunk":
		receiver.AgentMessageChunk(notification.SessionID, update.Content, update.MessageID)
	case "agent_thought_chunk":
		receiver.AgentThoughtChunk(notification.SessionID, update.Thought, update.Content)
	case "tool_call":
		receiver.ToolCall(notification.SessionID, update.ToolCall)
	case "tool_call_update":
		receiver.ToolCallUpdate(notification.SessionID, update.ToolCallUpdate)
	case "plan":
		receiver.Plan(notification.SessionID, update.Plan)
	case "available_commands_update":
		receiver.AvailableCommandsUpdate(notification.SessionID, update.AvailableCommands)
	case "current_mode_update":
		receiver.CurrentModeUpdate(notification.SessionID, update.CurrentMode)
	case "config_option_update":
		receiver.ConfigOptionUpdate(notification.SessionID, update.ConfigOption)
	case "session_info_update":
		receiver.SessionInfoUpdate(notification.SessionID, update.SessionInfo)
	case "usage_update":
		receiver.UsageUpdate(notification.SessionID, update.Usage)
	default:
		receiver.UnknownSessionUpdate(notification)
	}
}
