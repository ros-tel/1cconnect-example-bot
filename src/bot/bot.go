package bot

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"connect-companion/bot/messages"
	"connect-companion/bot/requests"
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

func processMessage(c *gin.Context, msg *messages.Message, chatState *database.Chat) (database.ChatState, error) {
	switch msg.MessageType {
	case messages.MESSAGE_TREATMENT_START_BY_USER:
		return chatState.CurrentState, nil
	case messages.MESSAGE_TREATMENT_START_BY_SPEC,
		messages.MESSAGE_TREATMENT_CLOSE,
		messages.MESSAGE_TREATMENT_CLOSE_ACTIVE:

		return msg.Start(database.STATE_GREETINGS)
	case messages.MESSAGE_TEXT:
		text := strings.ToLower(strings.TrimSpace(msg.Text))

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
			return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
		case database.STATE_MAIN_MENU:
			switch text {
			case "1", "общая информация о сервисе 1с-эдо":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_2, keyboardParting2)
			case "2", "использование сервиса 1с-эдо":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_1, keyboardParting1)
			case "3", "возникла ошибка при работе с эдо":
				return msg.Send(c, "Если при работе с сервисом у Вас возникла ошибка, необходимо перейти на официальный сайт сервиса в раздел «Техподдержка» http://1c-edo.ru/handbook/ и осуществить поиск по справочнику. Для поиска рекомендуется использовать информацию из текста ошибки.  Если поиск не дал результатов, рекомендуется связаться со специалистом для дальнейшего разбора ошибки.", database.STATE_PART_11, keyboardParting11)
			case "4", "организационные вопросы от партнеров по сервису 1с-эдо":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_12, keyboardParting12)
			case "5", "мой вопрос не по теме эдо":
				return msg.Send(c, "Вы обратились на линию поддержки сервиса 1С-ЭДО. Если ваш вопрос не связан с данным сервисом, рекомендуем обратиться в  соответствующую поддержку https://portal.1c.ru/", database.STATE_PART_10, keyboardParting10)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardMain)
			}
		case database.STATE_PART_1:
			switch text {

			case "1", "инструкции по работе с сервисом":
				msg.Send(c, "Инструкции по работе с сервисом 1С-ЭДО можно просмотреть на сайте сервиса в разделе «Техническая поддержка» http://1c-edo.ru/handbook/ . Видеоинструкции http://1c-edo.ru/handbook/all-videos/ . Для пользователей программных продуктов, использующих Библиотеку Электронных документов 1.1, следует воспользоваться руководством пользователя https://its.1c.ru/db/eldocs#content:102:hdoc.", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "подключение к эдо":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_5, keyboardParting5)
				// return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "настройка электронного документооборота":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_8, keyboardParting8)
				// return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "4", "приглашение контрагентов к обмену":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_4, keyboardParting4)
				// return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "работа с электронными документами":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_3, keyboardParting3)
				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "6", "внутренний документооборот":
				msg.Send(c, "Электронный документооборот позволяет оформить внутренние документы организации в электронном виде с использованием простой или усиленной квалифицированной подписи. Функционал внутреннего ЭДО реализован в БЭД (1С:Библиотеке электронных документов) версии 1.7.2. Для подключения внутреннего электронного документооборота воспользуйтесь инструкцией http://1c-edo.ru/handbook/22/4901/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting1)
			}
		case database.STATE_PART_2:
			switch text {
			case "1", "информация о сервисе":
				msg.Send(c, "С подробной информацией о сервисе можно ознакомиться, перейдя по ссылке https://portal.1c.ru/applications/30", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "стоимость сервиса":
				msg.Send(c, "Регистрация в сервисе 1С-ЭДО и получение входящих документов от контрагентов - бесплатно. Пользователям с действующими тарифами 1С:ИТС предоставляется право на отправку определенного количества пакетов электронных документов без дополнительной оплаты. С подробной информацией о стоимости сервиса можно ознакомиться по ссылке http://1c-edo.ru/handbook/19/3988/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "порядок подключения":
				msg.Send(c, "С необходимыми требованиями, а также порядком подключения можно ознакомиться, перейдя по ссылке http://1c-edo.ru/handbook/19/3986/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting2)
			}
		case database.STATE_PART_3:
			switch text {
			case "1", "получение электронных документов":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_6, keyboardParting6)
				// return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "как отправить электронный документ":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_7, keyboardParting7)
				// return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "настройка отражения в учёте электронных документов":
				msg.Send(c, "В типовых решениях поддерживается автоматический способ обработки входящих электронных документов. Если номенклатура контрагента сопоставлена или в электронном документе содержатся услуги, система по умолчанию самостоятельно создаёт документы учётной системы на основании данных входящего электронного документа. Для изменения способа обработки входящих электронных документов рекомендуем воспользоваться инструкцией http://1c-edo.ru/handbook/22/4231/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "4", "смена форматов электронных документов":
				msg.Send(c, "Сервис 1С-ЭДО имеет возможность формирования электронных документов во всех действующих форматах разработанных ФНС. Также поддерживается обмен различными видами документов в формате CML 2.08. С подробной инструкцией по смене форматов можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4116/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "повторное создание электронного документа на основании документа учета":
				msg.Send(c, "В программном продукте «1С» ведется связь между электронными документами и документами учета. Для повторного создания электронного документа на основании документа учётной системы следует воспользоваться инструкцией http://1c-edo.ru/handbook/28/4096/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "6", "аннулирование электронного документа":
				msg.Send(c, "Если электронный документ ошибочно признан и подписан обеими сторонами сделки, то для лишения этого документа юридической значимости, необходимо провести процедуру его аннулирования. С подробной инструкцией по аннулированию электронного документа можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4084/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "7", "приёмка товара с учётом выявленных расхождений":
				msg.Send(c, "Факт расхождения количества товаров при приёмке оформляется соответствующим актом. Для формирования электронного акта о расхождениях воспользуйтесь инструкцией http://1c-edo.ru/handbook/22/4769/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "8", "подписание электронного документа несколькими сертификатами":
				msg.Send(c, "Маршруты подписания предоставляют возможность гибкой настройки правил подписания в разрезе видов исходящих электронных документов. Данный функционал также делает возможным подписание исходящих электронных документов несколькими подписями по заранее заданному маршруту перед их отправкой контрагентам. С подробной инструкцией по настройке, а также использованию маршрутов подписания можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4079/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "9", "заполнение дополнительных полей в электронном документе":
				msg.Send(c, "В ЭДО часто используются дополнительные данные, которые не предусмотрены форматами ФНС, такие как номера и даты заказов, номера партий, спецификаций, доверенностей, т.е. любая дополнительная информация, которую может затребовать поставщик или покупатель. С подробной инструкцией по настройке дополнительных полей можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4635/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "10", "убрать неактуальные документы из текущих дел эдо":
				msg.Send(c, "При невозможности завершить ЭДО имеется техническая возможность изъять документ из активного документооборота. С подробной инструкцией, по принудительному закрытию документооборота, можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4043/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "11", "печать электронного документа":
				msg.Send(c, "Законодательство не требует печати электронных документов. Если такая необходимость имеется, то пользователь может произвести печать посредством сервисных механизмов ЭДО. Распечатанный экземпляр будет являться копией документа. Оригиналом считается непосредственно электронный документ. С подробной инструкцией по печати электронного документа можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4282/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "12", "запрос ответной подписи по электронному документу":
				msg.Send(c, "Установка и снятие запроса ответной подписи происходит в разрезе видов исходящих электронных документов. С подробной инструкцией по запросу ответной подписи можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3985/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "13", "где найти документы, обмен по которым завершен":
				msg.Send(c, "Документы, обмен по которым завершён, можно посмотреть в Архиве ЭДО или перейти в электронный документ из соответствующего документа учёта. С подробной инструкцией можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3979/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_PART_1, keyboardParting1)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting3)
			}
		case database.STATE_PART_4:
			switch text {
			case "1", "как узнать идентификатор участника эдо":
				msg.Send(c, "Идентификатор участника ЭДО - это уникальный номер, который выдает оператор при регистрации участника в системе ЭДО. По своей сути, идентификатор является адресом организации в системе обмена электронными документами и его может запросить контрагент или оператор для настройки роуминга. С подробной инструкцией, как узнать идентификатор участнику ЭДО, можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4813/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "настройка роуминга":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_9, keyboardParting9)
			case "3", "отправка приглашения контрагенту":
				msg.Send(c, "Перед началом обмена необходимо пригласить контрагентов к обмену электронными документами. С подробной инструкцией по отправке приглашений контрагентам можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3992/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "4", "получение и принятие приглашения от контрагента":
				msg.Send(c, "Для того чтобы получить входящее приглашение от контрагента необходимо перейти в Текущие дела ЭДО и нажать «Отправить и получить». С подробной инструкцией по получению и принятию приглашения от контрагента можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4051/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "как отозвать приглашение":
				msg.Send(c, "Если Ваша организация планирует прекратить обмен с контрагентом, следует отозвать ранее принятое приглашение к обмену электронными документами. С подробной инструкцией как отозвать приглашение можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4835/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "6", "настройка отправки по договорам":
				msg.Send(c, "В системе реализована возможность разграничить настройки отправки в разрезе договоров. Предварительно следует установить флаг в поле «Учёт по договорам» в функциональности программы. С подробной инструкцией как создать настройку отправки по договорам можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4974/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "вернуться в меню":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting4)
			}

		case database.STATE_PART_5:
			switch text {
			case "1", "порядок подключения":
				msg.Send(c, "С необходимыми требованиями, а также порядком подключения можно ознакомиться, перейдя по ссылке http://1c-edo.ru/handbook/19/3986/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "стоимость сервиса":
				msg.Send(c, "Регистрация в сервисе 1С-ЭДО и получение входящих документов от контрагентов - бесплатно. Пользователям с действующими тарифами 1С:ИТС предоставляется право на отправку определенного количества пакетов электронных документов без дополнительной оплаты. С подробной информацией о стоимости сервиса можно ознакомиться по ссылке http://1c-edo.ru/handbook/19/3988/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "какой криптопровайдер использовать":
				msg.Send(c, "Библиотека обмена электронными документами использует методы криптографии, предоставляемые платформой «1С:Предприятие» , т. е. криптопровайдеры, поддерживающие интерфейс CryptoAPI. Предлагаем использовать наиболее распространенные криптопровайдеры, сертифицированные ФСБ России: КриптоПро CSP или ViPNet CSP. С подробной информацией можно ознакомиться по ссылке http://1c-edo.ru/handbook/19/3991/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "4", "какая электронная подпись подойдёт для обмена с контрагентами":
				msg.Send(c, "Для использования электронного документооборота с контрагентами нужна усиленная квалифицированная электронная подпись, выданная аккредитованным удостоверяющим центром. Данная подпись должна соответствовать требованиям Федерального закона от 06.04.2011 N 63-ФЗ «Об электронной подписи». Список аккредитованных удостоверяющих центров https://digital.gov.ru/ru/activity/govservices/certification_authority/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "где получить электронную подпись":
				msg.Send(c, "Электронную подпись можно получить в одном из аккредитованых удостоверяющих центров https://digital.gov.ru/ru/activity/govservices/certification_authority/. Сертификат электронной подписи можно получить из программы 1С используя сервис 1С:Подпись. С подробной инструкцией по получению 1С:Подписи можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/4285/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "6", "можно ли использовать облачную электронную подпись":
				msg.Send(c, "Приложение «1С:Клиент ЭДО» для пользователей 1cfresh.com поддерживает возможность получения и использования облачного сертификата, исключительно через сервис 1С-Подпись. С более подробной информацией можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4347/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "7", "настройка электронного документооборота с контрагентами в 1с":
				msg.Send(c, "Для подключения к электронному документообороту необходимо создать Учётную запись ЭДО, после чего отправить приглашения к обмену контрагентам. С подробной инструкцией по подключению можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3992/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "8", "подключение внутреннего электронного документооборота":
				msg.Send(c, "Электронный документооборот позволяет оформить внутренние документы организации в электронном виде с использованием простой или усиленной квалифицированной подписи. Функционал внутреннего ЭДО реализован в БЭД (1С:Библиотеке электронных документов) версии 1.7.2. Для подключения внутреннего электронного документооборота воспользуйтесь инструкцией http://1c-edo.ru/handbook/22/4901/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_PART_1, keyboardParting1)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting5)
			}

		case database.STATE_PART_6:
			switch text {
			case "1", "как получить электронный документ":
				msg.Send(c, "Для получения электронных документов необходимо выполнить команду «Отправить и получить» из рабочего места Текущие дела ЭДО. С подробной инструкцией по получению и последующей обработки электронного документа можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/3987/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "не приходят входящие документы от контрагента из «диадок»":
				msg.Send(c, "Некоторые пользователи сервиса сталкиваются с проблемами в получении документов от контрагентов, использующих «Диадок». Обычно эта проблема вызвана наличием у пользователя сервиса 1С-ЭДО активной учетной записи в «Диадок». С более подробной информацией о причинах возникновения данной проблемы, а также пути её решения, можно узнать по ссылке http://1c-edo.ru/handbook/22/3894/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_PART_3, keyboardParting3)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting6)
			}

		case database.STATE_PART_7:
			switch text {
			case "1", "как отправить электронный документ":
				msg.Send(c, "Исходящие электронные документы формируются на основании документов учётной системы. Отправку электронного документа можно осуществить как из документа учёта, так и из рабочего места Текущие дела ЭДО. С подробной инструкцией по отправке электронных документов можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/3989/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "как отправить произвольный документ":
				msg.Send(c, "С подробной инструкцией по отправке неформализованных электронных документов можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3978/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "отправка документа в обособленное подразделение контрагента":
				msg.Send(c, "С подробной инструкцией по использованию одного идентификатора различными обособленными подразделениями можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3953/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_PART_3, keyboardParting3)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting7)
			}

		case database.STATE_PART_8:
			switch text {
			case "1", "создание учётной записи":
				msg.Send(c, "С подробной инструкцией по созданию Учётной записи можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3992/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "настройка обмена с контрагентом":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_4, keyboardParting4)
			case "3", "настройка роуминга":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_9, keyboardParting9)
			case "4", "настройка vipnet csp для работы с 1с-эдо":
				msg.Send(c, "С подробной инструкцией по настройке криптопровайдера ViPNet CSP можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3994/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "настройка криптопро для работы с 1с-эдо":
				msg.Send(c, "С подробной инструкцией по настройке криптопровайдера КриптоПРО CSP можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4182/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "6", "настройка клиент-серверного подписания документов":
				msg.Send(c, "В случае, если Вы используете программу 1С в серверном режиме, у Вас есть возможность настроить и серверное подписание электронных документов, т.е. когда и криптопровайдер, и электронные подписи установлены только на сервере. С подробной инструкцией по настройке клиент-серверного подписания документов можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/3624/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "7", "настройка форматов исходящих электронных документов":
				msg.Send(c, "Перед началом обмена с контрагентами необходимо настроить форматы исходящих электронных документов. С подробной инструкцией по смене форматов можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4116/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "8", "настройка уведомлений эдо":
				msg.Send(c, "Для своевременного оповещения пользователей, о событиях электронного документооборота, в сервисе 1С-ЭДО реализовано три типа уведомлений. С подробной инструкцией по настройке уведомлений можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3518/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "9", "настройка отражения электронных документов в учёте":
				msg.Send(c, "В типовых решениях поддерживается автоматический способ обработке входящих электронных документов. Если номенклатура контрагента сопоставлена или в электронном документе содержатся услуги, система по умолчанию самостоятельно создаёт документы учётной системы на основании данных входящего электронного документа. Для изменения способа обработки входящих электронных документов рекомендуем воспользоваться инструкцией http://1c-edo.ru/handbook/22/4231/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "10", "настройка отправки по договорам":
				msg.Send(c, "В системе реализована возможность разграничить настройки отправки в разрезе договоров. Предварительно следует установить флаг в поле «Учёт по договорам» в функциональности программы. С подробной инструкцией как создать настройку отправки по договорам можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/4974/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "11", "настройка отправки в обособленное подразделение(филиал)":
				msg.Send(c, "С подробной инструкцией по использованию одного идентификатора различными обособленными подразделениями можно ознакомиться по ссылке  http://1c-edo.ru/handbook/22/3953/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "12", "настройка запроса ответной подписи":
				msg.Send(c, "Запрос ответной подписи выставляется в разрезе видов исходящих электронных документов. С подробной инструкцией по настройке запроса ответной подписи можно ознакомиться по ссылке  http://1c-edo.ru/handbook/22/3985/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_PART_1, keyboardParting1)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting8)
			}

		case database.STATE_PART_9:
			switch text {
			case "1", "операторы поддерживающие автоматический роуминг в 1с-эдо":
				msg.Send(c, "С перечнем операторов, доступных для автоматического роуминга в 1С-ЭДО можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/2691/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "настройка роуминга в 1с-эдо":
				msg.Send(c, "Настройка роуминга, в зависимости от оператора ЭДО контрагента, может выполняться через отправку приглашения (автоматический роуминг) или через отправку заявки на ручную настройку роуминга из программы 1С. Более подробно можно ознакомиться по ссылке http://1c-edo.ru/handbook/28/2667/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "автоматический роуминг для пользователей 1с-такском":
				msg.Send(c, "Если оператор поддерживает технологию автоматического роуминга, то настройка обмена электронными документами выполняется путем отправки приглашения из программы 1С. С перечнем операторов, поддерживающих автоматический роуминг можно ознакомиться по ссылке http://1c-edo.ru/handbook/22/3176/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "4", "роуминг по заявке для пользователей 1с-такском":
				msg.Send(c, "Если оператор не поддерживает автоматический роуминг, необходимо подготовить и отправить заявку на настройку роуминга согласно инструкции http://1c-edo.ru/handbook/28/2668/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "удаление стороннего идентификатора в системе тензор":
				msg.Send(c, "При настройке ЭДО между пользователем сервиса и абонентом оператора Тензор в системе оператора Тензор для абонента сервиса создается так называемый роуминговый кабинет. Если пользователь сервиса получит новый идентификатор и попытается заново настроить обмен с абонентом Тензора, система оператора Тензор отвергнет приглашение. Для корректной работы в сервисе рекомендуем воспользоваться инструкцией http://1c-edo.ru/handbook/22/3549/", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "вернуться в меню":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting9)
			}

		case database.STATE_PART_10:
			switch text {
			case "1", "вернуться в меню":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			case "2", "закрыть обращение":
				return msg.CloseTreatment(c, BOT_PHRASE_BYE, database.STATE_GREETINGS)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting10)
			}

		case database.STATE_PART_11:
			switch text {
			case "1", "вернуться в меню":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			case "2", "закрыть обращение":
				return msg.CloseTreatment(c, BOT_PHRASE_BYE, database.STATE_MAIN_MENU)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting11)
			}

		case database.STATE_PART_12:
			switch text {
			case "1", "информационные письма по работе с сервисом 1с-эдо":
				return msg.Send(c, BOT_PHRASE_PART, database.STATE_PART_13, keyboardParting13)
			case "2", "справочник партнера по итс":
				msg.Send(c, "Со справочником партнёра по сервису 1С-ЭДО можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:547:hdoc", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "настройка обмена с софтехно":
				msg.Send(c, "Для перехода на ЭДО с фирмой «1С» партнеру необходимо подключиться к ЭДО и настроить обмен с ООО «Софтехно». Необходимый для этого функционал доступен пользователям программ 1С:Предприятие 8. С подробной информацией по подключению к сервису и отправки приглашения можно ознакомиться, перейдя по ссылке http://1c-edo.ru/handbook/22/3992/ Идентификаторы ООО «Софтехно»: 2AL-D8DFADD4-0979-4746-86CD-7C343548D0D1-00001 Такском; 2AE7AEC0B60-3A99-47EC-8124-915440940934 Калуга-Астрал.", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "4", "описание и возможности сервиса":
				msg.Send(c, "С подробным описанием, а также возможностями сервиса можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1110:hdoc", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "как партнеру начать продавать сервис?":
				msg.Send(c, "Со схемой организации работ по продажам и подключению сервиса, а также другой полезной информацией можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1113:hdoc", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "6", "инструкции и информация для работы с клиентами":
				msg.Send(c, "С конкурентными преимуществами, тарифными планами и другой полезной информацией можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1118:1", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "7", "документы и регламенты":
				msg.Send(c, "С руководством пользователя, шаблонами документов, информацией по оформлению безлимитных тарифов СпецЭДО можно ознакомиться, перейдя по ссылке https://its.1c.ru/db/partnerits#content:1123:1", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "8", "рекламные материалы":
				msg.Send(c, "Рекламные материалы доступны по ссылке https://its.1c.ru/db/partnerits#content:1217:hdoc", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting12)
			}

		case database.STATE_PART_13:
			switch text {
			case "1", "№15101 от 04.05.2012 новое решение «1с-такском» удобный обмен электронными счетами-фактурами и другими документами для пользователей «1с:предприятия 8»":
				msg.Send(c, "Новое решение «1С-Такском» удобный обмен электронными счетами-фактурами и другими документами для пользователей «1С:Предприятия 8» https://1c.ru/news/info.jsp?id=15101", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "2", "№15102 от 04.05.2012 новое решение «1с-такском» для обмена электронными счетами-фактурами и другими документами - новые возможности развития бизнеса для партнеров":
				msg.Send(c, "Новое решение «1С-Такском» для обмена электронными счетами-фактурами и другими документами - новые возможности развития бизнеса для партнеров https://1c.ru/news/info.jsp?id=15102", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "3", "№15894 от 23.11.2012 продление акции «1000 комплектов электронных документов в месяц бесплатно» для пользователей 1с:итс в сервисе «1с-такском» по 31 декабря 2012 г. и другие обновления проекта":
				msg.Send(c, "Продление акции «1000 комплектов электронных документов в месяц бесплатно» для пользователей 1С:ИТС в сервисе «1С-Такском» по 31 декабря 2012 г. и другие обновления проекта https://1c.ru/news/info.jsp?id=15894", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "4", "№19679 от 13.03.2015 о выпуске редакции 2.0 прикладного решения «1с:клиент эдо 8». об упрощенной схеме подключения пользователей «1с:клиент эдо 8» к обмену электронными документами":
				msg.Send(c, "О выпуске редакции 2.0 прикладного решения «1С:Клиент ЭДО 8». Об упрощенной схеме подключения пользователей «1С:Клиент ЭДО 8» к обмену электронными документами https://1c.ru/news/info.jsp?id=19679", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "5", "№22682 от 01.03.2017 информационная система «сопровождение клиентов по 1с-эдо» на портале итс":
				msg.Send(c, "Информационная система «Сопровождение клиентов по 1С-ЭДО» на Портале ИТС https://1c.ru/news/info.jsp?id=22682", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "6", "№23069 от 26.05.2017 использование универсального передаточного документа упд при расчетах с партнерам":
				msg.Send(c, "Использование универсального передаточного документа (УПД) при расчетах с партнерами https://1c.ru/news/info.jsp?id=23069", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "7", "№24031 от 22.01.2018 интернет-курс «эдо: станьте уверенным пользователем»":
				msg.Send(c, "Интернет-курс «ЭДО: станьте уверенным пользователем» https://1c.ru/news/info.jsp?id=24031", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "8", "№24188 от 27.02.2018 новое о роуминге для пользователей эдо в пп 1с":
				msg.Send(c, "Новое о роуминге для пользователей ЭДО в ПП 1С https://1c.ru/news/info.jsp?id=24188", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "9", "№25554 от 27.02.2019 фирма «1с» упрощает доступ к сервису 1с-эдо для всех пользователей":
				msg.Send(c, "Фирма «1С» упрощает доступ к сервису 1С-ЭДО для всех пользователей https://1c.ru/news/info.jsp?id=25554", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "10", "№25555 от 27.02.2019 нужна помощь партнеров в подключении пользователей 1с к 1с-эдо":
				msg.Send(c, "Нужна помощь партнеров в подключении пользователей 1С к 1С-ЭДО https://1c.ru/news/info.jsp?id=25555", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "11", "№26312 от 27.09.2019 мобильный клиент «1с:клиент эдо» для пользователей облачного сервиса 1с:фреш":
				msg.Send(c, "Мобильный клиент «1С:Клиент ЭДО» для пользователей облачного сервиса 1С:Фреш https://1c.ru/news/info.jsp?id=26312", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "12", "№27486 от 07.08.2020 оператор эдо ооо «такском» поддержал технологию 1с-эдо, дополнительно к 1с-такском":
				msg.Send(c, "Оператор ЭДО ООО «Такском» поддержал технологию 1С-ЭДО дополнительно к 1С-Такском https://1c.ru/news/info.jsp?id=27486", database.STATE_PARTING, nil)

				time.Sleep(3 * time.Second)

				return msg.Send(c, BOT_PHRASE_AGAIN, database.STATE_PARTING, keyboardParting)
			case "0", "возврат на шаг назад":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_PART_12, keyboardParting12)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting13)
			}

		case database.STATE_PARTING:
			switch text {
			case "1", "да":
				return msg.Send(c, BOT_PHRASE_GREETING, database.STATE_MAIN_MENU, keyboardMain)
			case "2", "нет":
				return msg.CloseTreatment(c, BOT_PHRASE_BYE, database.STATE_GREETINGS)
			case "00", "соединить со специалистом":
				return msg.RerouteTreatment(c, BOT_PHRASE_REROUTING, database.STATE_GREETINGS)
			default:
				return msg.Send(c, BOT_PHRASE_SORRY, chatState.CurrentState, keyboardParting)
			}
		}
	case messages.MESSAGE_FILE:
		return msg.StartAndReroute(database.STATE_GREETINGS)

	case messages.MESSAGE_CALL_START_TREATMENT:
		return msg.StartAndReroute(database.STATE_GREETINGS)
	}

	return database.STATE_DUMMY, errors.New("I don't know hat i mus do!")
}
