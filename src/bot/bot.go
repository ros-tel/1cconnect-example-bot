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
	BOT_PHRASE_GREETING  = "Здравствуйте! Это электронный помощник 1С. Для навигации в меню необходимо использовать только числовые значения - 1, 2, 3 и т.д. К какой категории можно отнести Ваш вопрос?"
	BOT_PHRASE_PART      = "Выберите наиболее подходящую тему. Для навигации в меню прошу использовать только числовые значения - 1, 2, 3 и т.д."
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
			{{Id: "1", Text: "Общая информация о сервисе 1С-ЭДО"}},
			{{Id: "2", Text: "Использование сервиса 1С-ЭДО"}},
			{{Id: "3", Text: "Возникла ошибка при работе с ЭДО"}},
			{{Id: "4", Text: "Организационные вопросы от партнеров по сервису 1С-ЭДО"}},
			{{Id: "5", Text: "Мой вопрос не по теме ЭДО"}},
		}
		keyboardParting1 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Инструкции по работе с сервисом"}},
			{{Id: "2", Text: "Подключение к ЭДО"}},
			{{Id: "3", Text: "Настройка электронного документооборота"}},
			{{Id: "4", Text: "Приглашение контрагентов к обмену"}},
			{{Id: "5", Text: "Работа с электронными документами"}},
			{{Id: "6", Text: "Внутренний документооборот"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
		}
		keyboardParting2 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Информация о сервисе"}},
			{{Id: "2", Text: "Стоимость сервиса"}},
			{{Id: "3", Text: "Порядок подключения"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
		}
		keyboardParting3 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Получение электронных документов"}},
			{{Id: "2", Text: "Как отправить электронный документ"}},
			{{Id: "3", Text: "Настройка отражения в учёте электронных документов"}},
			{{Id: "4", Text: "Смена форматов электронных документов"}},
			{{Id: "5", Text: "Повторное создание электронного документа на основании документа учета"}},
			{{Id: "6", Text: "Аннулирование электронного документа"}},
			{{Id: "7", Text: "Приёмка товара с учётом выявленных расхождений"}},
			{{Id: "8", Text: "Подписание электронного документа несколькими сертификатами"}},
			{{Id: "9", Text: "Заполнение дополнительных полей в электронном документе"}},
			{{Id: "10", Text: "Убрать неактуальные документы из Текущих дел ЭДО"}},
			{{Id: "11", Text: "Печать электронного документа"}},
			{{Id: "12", Text: "Запрос ответной подписи по электронному документу"}},
			{{Id: "13", Text: "Где найти документы, обмен по которым завершен"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
		}

		keyboardParting4 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Как узнать идентификатор участника ЭДО"}},
			{{Id: "2", Text: "Настройка роуминга"}},
			{{Id: "3", Text: "Отправка приглашения контрагенту"}},
			{{Id: "4", Text: "Получение и принятие приглашения от контрагента"}},
			{{Id: "5", Text: "Как отозвать приглашение"}},
			{{Id: "6", Text: "Настройка отправки по договорам"}},
			{{Id: "0", Text: "Вернуться в меню"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting5 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Порядок подключения"}},
			{{Id: "2", Text: "Стоимость сервиса"}},
			{{Id: "3", Text: "Какой криптопровайдер использовать"}},
			{{Id: "4", Text: "Какая электронная подпись подойдёт для обмена с контрагентами"}},
			{{Id: "5", Text: "Где получить электронную подпись"}},
			{{Id: "6", Text: "Можно ли использовать облачную электронную подпись"}},
			{{Id: "7", Text: "Настройка электронного документооборота с контрагентами в 1С"}},
			{{Id: "8", Text: "Подключение внутреннего электронного документооборота"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting6 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Как получить электронный документ"}},
			{{Id: "2", Text: "Не приходят входящие документы от контрагента из «Диадок»"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting7 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Как отправить электронный документ"}},
			{{Id: "2", Text: "Как отправить произвольный документ"}},
			{{Id: "3", Text: "Отправка документа в обособленное подразделение контрагента"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting8 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Создание учётной записи"}},
			{{Id: "2", Text: "Настройка обмена с контрагентом"}},
			{{Id: "3", Text: "Настройка роуминга"}},
			{{Id: "4", Text: "Настройка ViPNet CSP для работы с 1С-ЭДО"}},
			{{Id: "5", Text: "Настройка КриптоПРО для работы с 1С-ЭДО"}},
			{{Id: "6", Text: "Настройка клиент-серверного подписания документов"}},
			{{Id: "7", Text: "Настройка форматов исходящих электронных документов"}},
			{{Id: "8", Text: "Настройка уведомлений ЭДО"}},
			{{Id: "9", Text: "Настройка отражения электронных документов в учёте"}},
			{{Id: "10", Text: "Настройка отправки по договорам"}},
			{{Id: "11", Text: "Настройка отправки в обособленное подразделение(филиал)"}},
			{{Id: "12", Text: "Настройка запроса ответной подписи"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting9 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Операторы поддерживающие автоматический роуминг в 1С-ЭДО"}},
			{{Id: "2", Text: "Настройка роуминга в 1С-ЭДО"}},
			{{Id: "3", Text: "Автоматический роуминг для пользователей 1С-Такском"}},
			{{Id: "4", Text: "Роуминг по заявке для пользователей 1С-Такском"}},
			{{Id: "5", Text: "Удаление стороннего идентификатора в системе Тензор"}},
			{{Id: "0", Text: "Вернуться в меню"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting10 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Вернуться в меню"}},
			{{Id: "2", Text: "Закрыть обращение"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting11 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Вернуться в меню"}},
			{{Id: "2", Text: "Закрыть обращение"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting12 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Информационные письма по работе с сервисом 1С-ЭДО"}},
			{{Id: "2", Text: "Справочник партнера по ИТС"}},
			{{Id: "3", Text: "Настройка обмена с Софтехно"}},
			{{Id: "4", Text: "Описание и возможности сервиса"}},
			{{Id: "5", Text: "Как партнеру начать продавать сервис?"}},
			{{Id: "6", Text: "Инструкции и информация для работы с клиентами"}},
			{{Id: "7", Text: "Документы и регламенты"}},
			{{Id: "8", Text: "Рекламные материалы"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting13 := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "№15101 от 04.05.2012 Новое решение «1С-Такском» удобный обмен электронными счетами-фактурами и другими документами для пользователей «1С:Предприятия 8»"}},
			{{Id: "2", Text: "№15102 от 04.05.2012 Новое решение «1С-Такском» для обмена электронными счетами-фактурами и другими документами - новые возможности развития бизнеса для партнеров"}},
			{{Id: "3", Text: "№15894 от 23.11.2012 Продление акции «1000 комплектов электронных документов в месяц бесплатно» для пользователей 1С:ИТС в сервисе «1С-Такском» по 31 декабря 2012 г. и другие обновления проекта"}},
			{{Id: "4", Text: "№19679 от 13.03.2015 О выпуске редакции 2.0 прикладного решения «1С:Клиент ЭДО 8». Об упрощенной схеме подключения пользователей «1С:Клиент ЭДО 8» к обмену электронными документами"}},
			{{Id: "5", Text: "№22682 от 01.03.2017 Информационная система «Сопровождение клиентов по 1С-ЭДО» на Портале ИТС"}},
			{{Id: "6", Text: "№23069 от 26.05.2017 Использование универсального передаточного документа УПД при расчетах с партнерам"}},
			{{Id: "7", Text: "№24031 от 22.01.2018 Интернет-курс «ЭДО: станьте уверенным пользователем»"}},
			{{Id: "8", Text: "№24188 от 27.02.2018 Новое о роуминге для пользователей ЭДО в ПП 1С"}},
			{{Id: "9", Text: "№25554 от 27.02.2019 Фирма «1С» упрощает доступ к сервису 1С-ЭДО для всех пользователей"}},
			{{Id: "10", Text: "№25555 от 27.02.2019 Нужна помощь партнеров в подключении пользователей 1С к 1С-ЭДО"}},
			{{Id: "11", Text: "№26312 от 27.09.2019 Мобильный клиент «1С:Клиент ЭДО» для пользователей облачного сервиса 1С:Фреш"}},
			{{Id: "12", Text: "№27486 от 07.08.2020 Оператор ЭДО ООО «Такском» поддержал технологию 1С-ЭДО, дополнительно к 1С-Такском"}},
			{{Id: "0", Text: "Возврат на шаг назад"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		keyboardParting := &[][]requests.KeyboardKey{
			{{Id: "1", Text: "Да"}, {Id: "2", Text: "Нет"}},
			{{Id: "00", Text: "Соединить со специалистом"}},
		}

		switch chatState.CurrentState {
		case database.STATE_DUMMY, database.STATE_GREETINGS:
			_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

			return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
		case database.STATE_MAIN_MENU:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "общая информация о сервисе 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting2)

				return checkErrorForSend(msg, err, database.STATE_PART_2)

			case "2", "использование сервиса 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting1)

				return checkErrorForSend(msg, err, database.STATE_PART_1)

			case "3", "возникла ошибка при работе с эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Если при работе с сервисом у Вас возникла ошибка, необходимо перейти на официальный сайт сервиса в раздел «Техподдержка» http://1c-edo.ru/handbook/ и осуществить поиск по справочнику. Для поиска рекомендуется использовать информацию из текста ошибки.  Если поиск не дал результатов, рекомендуется связаться со специалистом для дальнейшего разбора ошибки.", keyboardParting11)

				return checkErrorForSend(msg, err, database.STATE_PART_11)

			case "4", "организационные вопросы от партнеров по сервису 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting12)

				return checkErrorForSend(msg, err, database.STATE_PART_12)

			case "5", "мой вопрос не по теме эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Вы обратились на линию поддержки сервиса 1С-ЭДО. Если ваш вопрос не связан с данным сервисом, рекомендуем обратиться в  соответствующую поддержку https://portal.1c.ru/", keyboardParting10)

				return checkErrorForSend(msg, err, database.STATE_PART_10)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardMain)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_1:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {

			case "1", "инструкции по работе с сервисом":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Инструкции по работе с сервисом 1С-ЭДО можно просмотреть на сайте сервиса в разделе «Техническая поддержка» http://1c-edo.ru/handbook/ . Видеоинструкции http://1c-edo.ru/handbook/all-videos/ . Для пользователей программных продуктов, использующих Библиотеку Электронных документов 1.1, следует воспользоваться руководством пользователя https://its.1c.ru/db/eldocs#content:102:hdoc.", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "2", "подключение к эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting5)

				return checkErrorForSend(msg, err, database.STATE_PART_5)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "3", "настройка электронного документооборота":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting8)

				return checkErrorForSend(msg, err, database.STATE_PART_8)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "4", "приглашение контрагентов к обмену":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting4)

				return checkErrorForSend(msg, err, database.STATE_PART_4)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "работа с электронными документами":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting3)

				return checkErrorForSend(msg, err, database.STATE_PART_3)
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "6", "внутренний документооборот":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Электронный документооборот позволяет оформить внутренние документы организации в электронном виде с использованием простой или усиленной квалифицированной подписи. Функционал внутреннего ЭДО реализован в БЭД (1С:Библиотеке электронных документов) версии 1.7.2. Для подключения внутреннего электронного документооборота воспользуйтесь инструкцией http://1c-edo.ru/handbook/22/4901/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting1)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_2:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "информация о сервисе":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробной информацией о сервисе можно ознакомиться, перейдя по ссылке https://portal.1c.ru/applications/30", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "2", "стоимость сервиса":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Регистрация в сервисе 1С-ЭДО и получение входящих документов от контрагентов - бесплатно. Пользователям с действующими тарифами 1С:ИТС предоставляется право на отправку определенного количества пакетов электронных документов без дополнительной оплаты. С подробной информацией о стоимости сервиса можно ознакомиться по ссылке http://1c-edo.ru/handbook/19/3988/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "3", "порядок подключения":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С необходимыми требованиями, а также порядком подключения можно ознакомиться, перейдя по ссылке http://1c-edo.ru/handbook/19/3986/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting2)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_3:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "получение электронных документов":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting6)

				return checkErrorForSend(msg, err, database.STATE_PART_6)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "2", "как отправить электронный документ":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting7)

				return checkErrorForSend(msg, err, database.STATE_PART_7)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "3", "настройка отражения в учёте электронных документов":

				_, err := SendMessage(c, msg.LineId, msg.UserId, "В типовых решениях поддерживается автоматический способ обработки входящих электронных документов. Если номенклатура контрагента сопоставлена или в электронном документе содержатся услуги, система по умолчанию самостоятельно создаёт документы учётной системы на основании данных входящего электронного документа. Для изменения способа обработки входящих электронных документов рекомендуем воспользоваться инструкцией http://1c-edo.ru/handbook/22/4231/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "4", "смена форматов электронных документов":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Сервис 1С-ЭДО имеет возможность формирования электронных документов во всех действующих форматах разработанных ФНС. Также поддерживается обмен различными видами документов в формате CML 2.08. С подробной инструкцией по смене форматов можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4116/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "повторное создание электронного документа на основании документа учета":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "В программном продукте «1С» ведется связь между электронными документами и документами учета. Для повторного создания электронного документа на основании документа учётной системы следует воспользоваться инструкцией http://1c-edo.ru/handbook/28/4096/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "6", "аннулирование электронного документа":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Если электронный документ ошибочно признан и подписан обеими сторонами сделки, то для лишения этого документа юридической значимости, необходимо провести процедуру его аннулирования. С подробной инструкцией по аннулированию электронного документа можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4084/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "7", "приёмка товара с учётом выявленных расхождений":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Факт расхождения количества товаров при приёмке оформляется соответствующим актом. Для формирования электронного акта о расхождениях воспользуйтесь инструкцией http://1c-edo.ru/handbook/22/4769/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "8", "подписание электронного документа несколькими сертификатами":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Маршруты подписания предоставляют возможность гибкой настройки правил подписания в разрезе видов исходящих электронных документов. Данный функционал также делает возможным подписание исходящих электронных документов несколькими подписями по заранее заданному маршруту перед их отправкой контрагентам. С подробной инструкцией по настройке, а также использованию маршрутов подписания можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4079/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "9", "заполнение дополнительных полей в электронном документе":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "В ЭДО часто используются дополнительные данные, которые не предусмотрены форматами ФНС, такие как номера и даты заказов, номера партий, спецификаций, доверенностей, т.е. любая дополнительная информация, которую может затребовать поставщик или покупатель. С подробной инструкцией по настройке дополнительных полей можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4635/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "10", "убрать неактуальные документы из текущих дел эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "При невозможности завершить ЭДО имеется техническая возможность изъять документ из активного документооборота. С подробной инструкцией, по принудительному закрытию документооборота, можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4043/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "11", "печать электронного документа":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Законодательство не требует печати электронных документов. Если такая необходимость имеется, то пользователь может произвести печать посредством сервисных механизмов ЭДО. Распечатанный экземпляр будет являться копией документа. Оригиналом считается непосредственно электронный документ. С подробной инструкцией по печати электронного документа можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4282/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "12", "запрос ответной подписи по электронному документу":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Установка и снятие запроса ответной подписи происходит в разрезе видов исходящих электронных документов. С подробной инструкцией по запросу ответной подписи можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3985/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "13", "где найти документы, обмен по которым завершен":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Документы, обмен по которым завершён, можно посмотреть в Архиве ЭДО или перейти в электронный документ из соответствующего документа учёта. С подробной инструкцией можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3979/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardParting1)

				return checkErrorForSend(msg, err, database.STATE_PART_1)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting3)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}
		case database.STATE_PART_4:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "как узнать идентификатор участника эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Идентификатор участника ЭДО - это уникальный номер, который выдает оператор при регистрации участника в системе ЭДО. По своей сути, идентификатор является адресом организации в системе обмена электронными документами и его может запросить контрагент или оператор для настройки роуминга. С подробной инструкцией, как узнать идентификатор участнику ЭДО, можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4813/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "2", "настройка роуминга":

				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting9)

				return checkErrorForSend(msg, err, database.STATE_PART_9)

			case "3", "отправка приглашения контрагенту":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Перед началом обмена необходимо пригласить контрагентов к обмену электронными документами. С подробной инструкцией по отправке приглашений контрагентам можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3992/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "4", "получение и принятие приглашения от контрагента":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Для того чтобы получить входящее приглашение от контрагента необходимо перейти в Текущие дела ЭДО и нажать «Отправить и получить». С подробной инструкцией по получению и принятию приглашения от контрагента можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4051/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "как отозвать приглашение":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Если Ваша организация планирует прекратить обмен с контрагентом, следует отозвать ранее принятое приглашение к обмену электронными документами. С подробной инструкцией как отозвать приглашение можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4835/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "6", "настройка отправки по договорам":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "В системе реализована возможность разграничить настройки отправки в разрезе договоров. Предварительно следует установить флаг в поле «Учёт по договорам» в функциональности программы. С подробной инструкцией как создать настройку отправки по договорам можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4974/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "вернуться в меню":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting4)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_5:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "порядок подключения":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С необходимыми требованиями, а также порядком подключения можно ознакомиться, перейдя по ссылке http://1c-edo.ru/handbook/19/3986/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "2", "стоимость сервиса":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Регистрация в сервисе 1С-ЭДО и получение входящих документов от контрагентов - бесплатно. Пользователям с действующими тарифами 1С:ИТС предоставляется право на отправку определенного количества пакетов электронных документов без дополнительной оплаты. С подробной информацией о стоимости сервиса можно ознакомиться по ссылке http://1c-edo.ru/handbook/19/3988/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "3", "какой криптопровайдер использовать":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Библиотека обмена электронными документами использует методы криптографии, предоставляемые платформой «1С:Предприятие» , т. е. криптопровайдеры, поддерживающие интерфейс CryptoAPI. Предлагаем использовать наиболее распространенные криптопровайдеры, сертифицированные ФСБ России: КриптоПро CSP или ViPNet CSP. С подробной информацией можно ознакомиться по ссылке http://1c-edo.ru/handbook/19/3991/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "4", "какая электронная подпись подойдёт для обмена с контрагентами":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Для использования электронного документооборота с контрагентами нужна усиленная квалифицированная электронная подпись, выданная аккредитованным удостоверяющим центром. Данная подпись должна соответствовать требованиям Федерального закона от 06.04.2011 N 63-ФЗ «Об электронной подписи». Список аккредитованных удостоверяющих центров https://digital.gov.ru/ru/activity/govservices/certification_authority/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "где получить электронную подпись":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Электронную подпись можно получить в одном из аккредитованых удостоверяющих центров https://digital.gov.ru/ru/activity/govservices/certification_authority/. Сертификат электронной подписи можно получить из программы 1С используя сервис 1С:Подпись. С подробной инструкцией по получению 1С:Подписи можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4285/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "6", "можно ли использовать облачную электронную подпись":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Приложение «1С:Клиент ЭДО» для пользователей 1cfresh.com поддерживает возможность получения и использования облачного сертификата, исключительно через сервис 1С-Подпись. С более подробной информацией можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4347/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "7", "настройка электронного документооборота с контрагентами в 1с":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Для подключения к электронному документообороту необходимо создать Учётную запись ЭДО, после чего отправить приглашения к обмену контрагентам. С подробной инструкцией по подключению можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3992/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "8", "подключение внутреннего электронного документооборота":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Электронный документооборот позволяет оформить внутренние документы организации в электронном виде с использованием простой или усиленной квалифицированной подписи. Функционал внутреннего ЭДО реализован в БЭД (1С:Библиотеке электронных документов) версии 1.7.2. Для подключения внутреннего электронного документооборота воспользуйтесь инструкцией http://1c-edo.ru/handbook/22/4901/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardParting1)

				return checkErrorForSend(msg, err, database.STATE_PART_1)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting5)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_6:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "как получить электронный документ":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Для получения электронных документов необходимо выполнить команду «Отправить и получить» из рабочего места Текущие дела ЭДО. С подробной инструкцией по получению и последующей обработки электронного документа можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/3987/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "2", "не приходят входящие документы от контрагента из «диадок»":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Некоторые пользователи сервиса сталкиваются с проблемами в получении документов от контрагентов, использующих «Диадок». Обычно эта проблема вызвана наличием у пользователя сервиса 1С-ЭДО активной учетной записи в «Диадок». С более подробной информацией о причинах возникновения данной проблемы, а также пути её решения, можно узнать по ссылке http://1c-edo.ru/handbook/22/3894/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardParting3)

				return checkErrorForSend(msg, err, database.STATE_PART_3)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting6)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_7:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "как отправить электронный документ":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Исходящие электронные документы формируются на основании документов учётной системы. Отправку электронного документа можно осуществить как из документа учёта, так и из рабочего места Текущие дела ЭДО. С подробной инструкцией по отправке электронных документов можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/3989/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "2", "как отправить произвольный документ":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробной инструкцией по отправке неформализованных электронных документов можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3978/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "3", "отправка документа в обособленное подразделение контрагента":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробной инструкцией по использованию одного идентификатора различными обособленными подразделениями можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3953/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardParting3)

				return checkErrorForSend(msg, err, database.STATE_PART_3)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting7)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_8:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "создание учётной записи":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробной инструкцией по созданию Учётной записи можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3992/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "2", "настройка обмена с контрагентом":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting4)

				return checkErrorForSend(msg, err, database.STATE_PART_4)

			case "3", "настройка роуминга":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting9)

				return checkErrorForSend(msg, err, database.STATE_PART_9)

			case "4", "настройка vipnet csp для работы с 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробной инструкцией по настройке криптопровайдера ViPNet CSP можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3994/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "настройка криптопро для работы с 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробной инструкцией по настройке криптопровайдера КриптоПРО CSP можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4182/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "6", "настройка клиент-серверного подписания документов":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "В случае, если Вы используете программу 1С в серверном режиме, у Вас есть возможность настроить и серверное подписание электронных документов, т.е. когда и криптопровайдер, и электронные подписи установлены только на сервере. С подробной инструкцией по настройке клиент-серверного подписания документов можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/3624/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "7", "настройка форматов исходящих электронных документов":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Перед началом обмена с контрагентами необходимо настроить форматы исходящих электронных документов. С подробной инструкцией по смене форматов можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4116/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "8", "настройка уведомлений эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Для своевременного оповещения пользователей, о событиях электронного документооборота, в сервисе 1С-ЭДО реализовано три типа уведомлений. С подробной инструкцией по настройке уведомлений можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3518/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "9", "настройка отражения электронных документов в учёте":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "В типовых решениях поддерживается автоматический способ обработке входящих электронных документов. Если номенклатура контрагента сопоставлена или в электронном документе содержатся услуги, система по умолчанию самостоятельно создаёт документы учётной системы на основании данных входящего электронного документа. Для изменения способа обработки входящих электронных документов рекомендуем воспользоваться инструкцией http://1c-edo.ru/handbook/22/4231/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "10", "настройка отправки по договорам":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "В системе реализована возможность разграничить настройки отправки в разрезе договоров. Предварительно следует установить флаг в поле «Учёт по договорам» в функциональности программы. С подробной инструкцией как создать настройку отправки по договорам можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4974/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "11", "настройка отправки в обособленное подразделение(филиал)":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробной инструкцией по использованию одного идентификатора различными обособленными подразделениями можно ознакомиться по ссылке  http://1c-edo.ru/handbook/22/3953/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "12", "настройка запроса ответной подписи":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Запрос ответной подписи выставляется в разрезе видов исходящих электронных документов. С подробной инструкцией по настройке запроса ответной подписи можно ознакомиться по ссылке  http://1c-edo.ru/handbook/22/3985/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardParting1)

				return checkErrorForSend(msg, err, database.STATE_PART_1)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting8)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_9:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "операторы поддерживающие автоматический роуминг в 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С перечнем операторов, доступных для автоматического роуминга в 1С-ЭДО можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/2691/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "2", "настройка роуминга в 1с-эдо":

				_, err := SendMessage(c, msg.LineId, msg.UserId, "Настройка роуминга, в зависимости от оператора ЭДО контрагента, может выполняться через отправку приглашения (автоматический роуминг) или через отправку заявки на ручную настройку роуминга из программы 1С. Более подробно можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/2667/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "3", "автоматический роуминг для пользователей 1с-такском":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Если оператор поддерживает технологию автоматического роуминга, то настройка обмена электронными документами выполняется путем отправки приглашения из программы 1С. С перечнем операторов, поддерживающих автоматический роуминг можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3176/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "4", "роуминг по заявке для пользователей 1с-такском":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Если оператор не поддерживает автоматический роуминг, необходимо подготовить и отправить заявку на настройку роуминга согласно инструкции http://1c-edo.ru/handbook/28/2668/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "удаление стороннего идентификатора в системе тензор":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "При настройке ЭДО между пользователем сервиса и абонентом оператора Тензор в системе оператора Тензор для абонента сервиса создается так называемый роуминговый кабинет. Если пользователь сервиса получит новый идентификатор и попытается заново настроить обмен с абонентом Тензора, система оператора Тензор отвергнет приглашение. Для корректной работы в сервисе рекомендуем воспользоваться инструкцией http://1c-edo.ru/handbook/22/3549/", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "вернуться в меню":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting9)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_10:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "вернуться в меню":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)

			case "2", "закрыть обращение":

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_BYE, nil)

				time.Sleep(1000 * time.Millisecond)

				_, err := CloseTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting10)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_11:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "вернуться в меню":

				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)

			case "2", "закрыть обращение":

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_BYE, nil)

				time.Sleep(1000 * time.Millisecond)

				_, err := CloseTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting11)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_12:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "информационные письма по работе с сервисом 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_PART, keyboardParting13)

				return checkErrorForSend(msg, err, database.STATE_PART_13)

			case "2", "справочник партнера по итс":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Со справочником партнёра по сервису 1С-ЭДО можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:547:hdoc", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "3", "настройка обмена с софтехно":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Для перехода на ЭДО с фирмой «1С» партнеру необходимо подключиться к ЭДО и настроить обмен с ООО «Софтехно». Необходимый для этого функционал доступен пользователям программ 1С:Предприятие 8. С подробной информацией по подключению к сервису и отправки приглашения можно ознакомиться, перейдя по ссылке http://1c-edo.ru/handbook/22/3992/ Идентификаторы ООО «Софтехно»: 2AL-D8DFADD4-0979-4746-86CD-7C343548D0D1-00001 Такском; 2AE7AEC0B60-3A99-47EC-8124-915440940934 Калуга-Астрал.", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "4", "описание и возможности сервиса":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С подробным описанием, а также возможностями сервиса можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1110:hdoc", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "как партнеру начать продавать сервис?":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Со схемой организации работ по продажам и подключению сервиса, а также другой полезной информацией можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1113:hdoc", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "6", "инструкции и информация для работы с клиентами":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С конкурентными преимуществами, тарифными планами и другой полезной информацией можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1118:1", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "7", "документы и регламенты":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "С руководством пользователя, шаблонами документов, информацией по оформлению безлимитных тарифов СпецЭДО можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1123:1", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "8", "рекламные материалы":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Рекламные материалы доступны по ссылке https://its.1c.ru/db/partnerits#content:1217:hdoc", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting12)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PART_13:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "№15101 от 04.05.2012 новое решение «1с-такском» удобный обмен электронными счетами-фактурами и другими документами для пользователей «1с:предприятия 8»":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Новое решение «1С-Такском» удобный обмен электронными счетами-фактурами и другими документами для пользователей «1С:Предприятия 8» https://1c.ru/news/info.jsp?id=15101", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "2", "№15102 от 04.05.2012 новое решение «1с-такском» для обмена электронными счетами-фактурами и другими документами - новые возможности развития бизнеса для партнеров":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Новое решение «1С-Такском» для обмена электронными счетами-фактурами и другими документами - новые возможности развития бизнеса для партнеров https://1c.ru/news/info.jsp?id=15102", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)
			case "3", "№15894 от 23.11.2012 продление акции «1000 комплектов электронных документов в месяц бесплатно» для пользователей 1с:итс в сервисе «1с-такском» по 31 декабря 2012 г. и другие обновления проекта":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Продление акции «1000 комплектов электронных документов в месяц бесплатно» для пользователей 1С:ИТС в сервисе «1С-Такском» по 31 декабря 2012 г. и другие обновления проекта https://1c.ru/news/info.jsp?id=15894", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "4", "№19679 от 13.03.2015 о выпуске редакции 2.0 прикладного решения «1с:клиент эдо 8». об упрощенной схеме подключения пользователей «1с:клиент эдо 8» к обмену электронными документами":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "О выпуске редакции 2.0 прикладного решения «1С:Клиент ЭДО 8». Об упрощенной схеме подключения пользователей «1С:Клиент ЭДО 8» к обмену электронными документами https://1c.ru/news/info.jsp?id=19679", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "5", "№22682 от 01.03.2017 информационная система «сопровождение клиентов по 1с-эдо» на портале итс":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Информационная система «Сопровождение клиентов по 1С-ЭДО» на Портале ИТС https://1c.ru/news/info.jsp?id=22682", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "6", "№23069 от 26.05.2017 использование универсального передаточного документа упд при расчетах с партнерам":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Использование универсального передаточного документа (УПД) при расчетах с партнерами https://1c.ru/news/info.jsp?id=23069", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "7", "№24031 от 22.01.2018 интернет-курс «эдо: станьте уверенным пользователем»":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Интернет-курс «ЭДО: станьте уверенным пользователем» https://1c.ru/news/info.jsp?id=24031", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "8", "№24188 от 27.02.2018 новое о роуминге для пользователей эдо в пп 1с":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Новое о роуминге для пользователей ЭДО в ПП 1С https://1c.ru/news/info.jsp?id=24188", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "9", "№25554 от 27.02.2019 фирма «1с» упрощает доступ к сервису 1с-эдо для всех пользователей":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Фирма «1С» упрощает доступ к сервису 1С-ЭДО для всех пользователей https://1c.ru/news/info.jsp?id=25554", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "10", "№25555 от 27.02.2019 нужна помощь партнеров в подключении пользователей 1с к 1с-эдо":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Нужна помощь партнеров в подключении пользователей 1С к 1С-ЭДО https://1c.ru/news/info.jsp?id=25555", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "11", "№26312 от 27.09.2019 мобильный клиент «1с:клиент эдо» для пользователей облачного сервиса 1с:фреш":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Мобильный клиент «1С:Клиент ЭДО» для пользователей облачного сервиса 1С:Фреш https://1c.ru/news/info.jsp?id=26312", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "12", "№27486 от 07.08.2020 оператор эдо ооо «такском» поддержал технологию 1с-эдо, дополнительно к 1с-такском":
				_, err := SendMessage(c, msg.LineId, msg.UserId, "Оператор ЭДО ООО «Такском» поддержал технологию 1С-ЭДО дополнительно к 1С-Такском https://1c.ru/news/info.jsp?id=27486", nil)

				time.Sleep(3 * time.Second)

				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_AGAIN, keyboardParting)

				return checkErrorForSend(msg, err, database.STATE_PARTING)

			case "0", "возврат на шаг назад":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardParting12)

				return checkErrorForSend(msg, err, database.STATE_PART_12)

			case "00", "соединить со специалистом":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_REROUTING, nil)

				time.Sleep(500 * time.Millisecond)

				_, err := RerouteTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)

			default:
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_SORRY, keyboardParting13)

				return checkErrorForSend(msg, err, chatState.CurrentState)
			}

		case database.STATE_PARTING:
			switch strings.ToLower(strings.TrimSpace(msg.Text)) {
			case "1", "да":
				_, err := SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_GREETING, keyboardMain)

				return checkErrorForSend(msg, err, database.STATE_MAIN_MENU)
			case "2", "нет":
				_, _ = SendMessage(c, msg.LineId, msg.UserId, BOT_PHRASE_BYE, nil)

				time.Sleep(1000 * time.Millisecond)

				_, err := CloseTreatment(msg.LineId, msg.UserId)

				return checkErrorForSend(msg, err, database.STATE_GREETINGS)
			case "00", "соединить со специалистом":
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
