package bot

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"connect-companion/bot/messages"
	"connect-companion/bot/requests"
	"connect-companion/config"
	"connect-companion/database"
	"connect-companion/logger"

	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v7"
)

const (
	BOT_PHRASE_GREETING = "Здравсвуйте! Это электронный помощник 1С. Какой у вас вопрос?"
	BOT_PHRASE_PART     = "Выберите наиболее подходящую тему"

	BOT_PHRASE_SORRY     = "Извините, но я вас не понимаю. Выберите, пожалуйста, один из вариантов:"
	BOT_PHRASE_AGAIN     = "У вас остались вопросы?"
	BOT_PHRASE_REROUTING = "Сейчас переведу, секундочку."
	BOT_PHRASE_BYE       = "Спасибо за обращение!"
)

var (
	cnf = &config.Conf{}
)

func Configure(c *config.Conf) {
	cnf = c
}

func Receive(c *gin.Context) {
	var msg messages.Message
	if err := c.BindJSON(&msg); err != nil {
		logger.Warning("Error while receive message", err)

		c.Status(http.StatusBadRequest)
		return
	}

	logger.Debug("Receive message:", msg)

	// Реагируем только на сообщения пользователя
	if (msg.MessageType == messages.MESSAGE_TEXT || msg.MessageType == messages.MESSAGE_FILE) && msg.MessageAuthor != nil && msg.UserId != *msg.MessageAuthor {
		c.Status(http.StatusOK)
		return
	}

	cCp := c.Copy()
	go func(cCp *gin.Context, msg messages.Message) {
		chatState := getState(c, &msg)

		newState, err := processMessage(c, &msg, &chatState)
		if err != nil {
			logger.Warning("Error processMessage", err)
		}

		err = changeState(c, &msg, &chatState, newState)
		if err != nil {
			logger.Warning("Error changeState", err)
		}
	}(cCp, msg)

	c.Status(http.StatusOK)
}

func getState(c *gin.Context, msg *messages.Message) database.Chat {
	db := c.MustGet("db").(*redis.Client)

	var chatState database.Chat

	dbStateKey := database.PREFIX_STATE + msg.UserId.String() + ":" + msg.LineId.String()

	dbStateRaw, err := db.Get(dbStateKey).Bytes()
	if err == redis.Nil {
		logger.Info("No state in db for " + msg.UserId.String() + ":" + msg.LineId.String())

		chatState = database.Chat{
			PreviousState: database.STATE_GREETINGS,
			CurrentState:  database.STATE_GREETINGS,
		}
	} else if err != nil {
		logger.Warning("Error while reading state from redis", err)
	} else {
		err = json.Unmarshal(dbStateRaw, &chatState)
		if err != nil {
			logger.Warning("Error while decoding state", err)
		}
	}

	return chatState
}

func changeState(c *gin.Context, msg *messages.Message, chatState *database.Chat, toState database.ChatState) error {
	db := c.MustGet("db").(*redis.Client)

	chatState.PreviousState = chatState.CurrentState
	chatState.CurrentState = toState

	data, err := json.Marshal(chatState)
	if err != nil {
		logger.Warning("Error while change state to db", err)

		return err
	}

	dbStateKey := database.PREFIX_STATE + msg.UserId.String() + ":" + msg.LineId.String()

	result, err := db.Set(dbStateKey, data, database.EXPIRE).Result()
	logger.Debug("Write state to db result", result)
	if err != nil {
		logger.Warning("Error while write state to db", err)
	}

	return nil
}

func checkErrorForSend(msg *messages.Message, err error, nextState database.ChatState) (database.ChatState, error) {
	if err != nil {
		logger.Warning("Get error while send message to line", msg.LineId, "for user", msg.UserId, "with error", err)
		return database.STATE_GREETINGS, err
	}

	return nextState, nil
}

func processMessage(c *gin.Context, msg *messages.Message, chatState *database.Chat) (database.ChatState, error) {
	switch msg.MessageType {
	case messages.MESSAGE_TREATMENT_START_BY_USER:
		return chatState.CurrentState, nil
	case messages.MESSAGE_TREATMENT_START_BY_SPEC,
		messages.MESSAGE_TREATMENT_CLOSE,
		messages.MESSAGE_TREATMENT_CLOSE_ACTIVE,
		messages.MESSAGE_TREATMENT_CLOSE_DEL_LINE,
		messages.MESSAGE_TREATMENT_CLOSE_DEL_SUBS,
		messages.MESSAGE_TREATMENT_CLOSE_DEL_USER:
		_, err := HideKeyboard(msg.LineId, msg.UserId)

		return checkErrorForSend(msg, err, database.STATE_GREETINGS)
	case messages.MESSAGE_TEXT:
		keyboardMain := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Пункт 1"}},
			{{Id: "2", Text: "Пункт 2"}},
			{{Id: "3", Text: "Пункт 3"}},
			{{Id: "4", Text: "Пункт 4"}},
		}
		keyboardParting1 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Пункт 1"}},
			{{Id: "2", Text: "Пункт 2"}},
			{{Id: "3", Text: "Пункт 3"}},
			{{Id: "9", Text: "Возврат на шаг назад"}},
			{{Id: "0", Text: "Соединить со специалистом"}},
		}
		keyboardParting2 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Пункт 1"}},
			{{Id: "2", Text: "Пункт 2"}},
			{{Id: "3", Text: "Пункт 3"}},
			{{Id: "4", Text: "Пункт 4"}},
			{{Id: "5", Text: "Пункт 5"}},
			{{Id: "6", Text: "Пункт 6"}},
			{{Id: "7", Text: "Пункт 7"}},
			{{Id: "8", Text: "Пункт 8"}},
			{{Id: "9", Text: "Возврат на шаг назад"}},
			{{Id: "0", Text: "Соединить со специалистом"}},
		}
		keyboardParting3 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Пункт 1"}},
			{{Id: "3", Text: "Пункт 3"}},
			{{Id: "9", Text: "Возврат на шаг назад"}},
			{{Id: "0", Text: "Соединить со специалистом"}},
		}
		keyboardParting4 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Пункт 1"}},
			{{Id: "9", Text: "Возврат на шаг назад"}},
			{{Id: "0", Text: "Соединить со специалистом"}},
		}

		keyboardParting := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Да"}, {Id: "2", Text: "Нет"}},
			{{Id: "0", Text: "Соединить со специалистом"}},
		}

		switch chatState.CurrentState {
		case database.STATE_DUMMY, database.STATE_GREETINGS:
			_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

			return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
		case database.STATE_MAIN_MENU:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "пункт 1":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting1)

				return checkErrorForSend(msg, err, database.STATE_PART_1)
			case "2", "пункт 2":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting2)

				return checkErrorForSend(msg, err, database.STATE_PART_2)
			case "3", "пункт 3":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting3)

				return checkErrorForSend(msg, err, database.STATE_PART_3)
			case "4", "пункт 4":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting4)

				return checkErrorForSend(msg, err, database.STATE_PART_4)
			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardMain)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_1:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "пункт 1":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 1 > Пункт 1", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "2", "пункт 2":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 1 > Пункт 2", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "3", "пункт 3":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 1 > Пункт 3", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "9", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
			case "0", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting1)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_2:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "пункт 1":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 1", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "2", "пункт 2":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 2", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "3", "пункт 3":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 3", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "4", "пункт 4":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 4", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "5", "пункт 5":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 5", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "6", "пункт 6":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 6", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "7", "пункт 7":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 7", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "8", "пункт 8":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 2 > Пункт 8", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "9", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			case "0", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting2)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_3:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "пункт 1":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 3 > Пункт 1", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "3", "пункт 3":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 3 > Пункт 3", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "9", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
			case "0", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting3)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_4:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "пункт 1":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Ответ на Пункт 4 > Пункт 1", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "9", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
			case "0", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting4)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PARTING:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "да":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
			case "2", "нет":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_BYE, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := CloseTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			case "0", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		}
	case messages.MESSAGE_FILE:
		_, err := HideKeyboard(msg.LineId, msg.UserId)
		_, err = RerouteTreatment(msg.LineId, msg.UserId)

		return checkErrorForSend(msg, err, database.STATE_GREETINGS)
	}

	return database.STATE_DUMMY, errors.New("I don't know hat i mus do!")
}
