package utils

import (
	"log"

	"github.com/gotd/td/tg"
)

// PrintMessages prints all messages from MessagesMessagesClass.
func PrintMessages(messagesClass tg.MessagesMessagesClass) {
	switch messages := messagesClass.(type) {
	case *tg.MessagesMessages:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				// log.Printf("message: Date %v, FromID %v, MessageID %v, Message %v", time.Unix(int64(m.Date), 0), m.FromID, m.ID, m.Message)
				log.Printf("message: Date %v, FromID %v, MessageID %v, Message %v", m.Date, m.FromID, m.ID, m.Message)
			default:
				log.Printf("unknown message class: %T", m)
			}
		}
	case *tg.MessagesMessagesSlice:
		for _, mc := range messages.Messages {
			switch m := mc.(type) {
			case *tg.Message:
				// log.Printf("message: Date %v, FromID %v, MessageID %v, Message %v", time.Unix(int64(m.Date), 0), m.FromID, m.ID, m.Message)
				log.Printf("message: Date %v, FromID %v, MessageID %v, Message %v", (m.Date), m.FromID, m.ID, m.Message)
			default:
				log.Printf("unknown message class: %T", m)
			}
		}
	default:
		log.Printf("unknown messagesmessages class: %T", messages)
	}
}
