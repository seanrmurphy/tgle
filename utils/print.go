package utils

import (
	"github.com/gotd/td/tg"
	"go.uber.org/zap"
)

// PrintMessages prints all messages from MessagesMessagesClass.
func PrintMessages(messagesClass tg.MessagesMessagesClass, summary bool, lg *zap.Logger) {
	var messages []tg.MessageClass
	switch messagesWrapper := messagesClass.(type) {
	case *tg.MessagesMessages:
		messages = messagesWrapper.Messages
	case *tg.MessagesMessagesSlice:
		messages = messagesWrapper.Messages
	default:
		lg.Sugar().Warnf("messagesmessages class: %T", messages)
		return
	}
	for _, mc := range messages {
		switch m := mc.(type) {
		case *tg.Message:
			if summary {
				lg.Sugar().Debugf("message: Date %v, FromID %v, MessageID %v, Message %v", m.Date, m.FromID, m.ID, m.Message)
			} else {
				lg.Sugar().Debugf("message: %v", m)
			}
		default:
			lg.Sugar().Warnf("unknown message class: %T", m)
		}
	}
}

// PrintMessages prints all messages from MessagesMessagesClass.
// func PrintMessages(messagesClass tg.MessagesMessagesClass, summary bool, lg *zap.Logger) {
// 	switch messages := messagesClass.(type) {
// 	case *tg.MessagesMessages:
// 		for _, mc := range messages.Messages {
// 			switch m := mc.(type) {
// 			case *tg.Message:
// 				if summary {
// 					lg.Sugar().Debugf("message: Date %v, FromID %v, MessageID %v, Message %v", m.Date, m.FromID, m.ID, m.Message)
// 				} else {
// 					lg.Sugar().Debugf("message: %v", m)
// 				}
// 			default:
// 				lg.Sugar().Warnf("unknown message class: %T", m)
// 			}
// 		}
// 	case *tg.MessagesMessagesSlice:
// 		for _, mc := range messages.Messages {
// 			switch m := mc.(type) {
// 			case *tg.Message:
// 				if summary {
// 					lg.Sugar().Debugf("message: Date %v, FromID %v, MessageID %v, Message %v", m.Date, m.FromID, m.ID, m.Message)
// 				} else {
// 					lg.Sugar().Debugf("message: %v", m)
// 				}
// 			default:
// 				lg.Sugar().Warnf("unknown message class: %T", m)
// 			}
// 		}
// 	default:
// 		lg.Sugar().Warnf("messagesmessages class: %T", messages)
// 	}
// }
