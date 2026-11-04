package bot

// User-facing bot messages (docs/06-telegram-bot.md). The bot speaks only
// Russian, like the app.
const (
	textIntro       = "Это бот приложения Quoker. Он работает только вместе с приложением по приглашению."
	textLinked      = "Готово! Quoker привязан к %s. Теперь добавьте меня в канал или группу, куда хотите отправлять видео.\n\nГруппа — добавьте меня участником.\nКанал — назначьте меня администратором с правом публиковать сообщения."
	textLinkTaken   = "Этот Telegram уже привязан к другому устройству Quoker. Отвяжите его там или обратитесь к администратору."
	textLinkExpired = "Ссылка устарела. Откройте Quoker и нажмите „Привязать Telegram“ ещё раз."
	textBlocked     = "Доступ закрыт администратором."
	textUnknown     = "Не знаю такой команды. Список — /help"

	textGroupHello    = "Привет! %s может отправлять сюда видео из Quoker. Другим участникам — команда /connect."
	textGroupNeedLink = "Чтобы отправлять сюда видео, сначала привяжите Quoker к своему Telegram, затем отправьте /connect"
	textConnected     = "Готово, „%s“ появится в Quoker у %s"
	textChannelNoPost = "В канале „%s“ у меня нет права публиковать сообщения."

	textHelp = "Видео отправляются из приложения Quoker.\n\n" +
		"Группа — добавьте меня в неё; другим участникам — команда /connect в группе.\n" +
		"Канал — назначьте меня администратором с правом публиковать сообщения."
	textAdminHelp = "\n\nКоманды администратора:\n" +
		"/invite — инвайт на 7 дней, /invite admin — с ролью администратора\n" +
		"/users — пользователи\n" +
		"/block <id>, /unblock <id> — блокировка\n" +
		"/status — состояние системы\n" +
		"/cookies — пришлите cookies.txt с этой подписью\n" +
		"/workers — воркеры\n" +
		"/allow_worker <id> <fingerprint> — разрешить воркер\n" +
		"/revoke_worker <id> — отозвать воркер"

	textInvite          = "Инвайт: %s\nРоль: %s\nДействует до %s МСК"
	textNoUsers         = "Пользователей нет."
	textBlockedUser     = "Пользователь #%d заблокирован."
	textUnblockedUser   = "Пользователь #%d разблокирован."
	textCantBlockSelf   = "Себя заблокировать нельзя."
	textNoSuchUser      = "Нет пользователя #%s."
	textUsageBlock      = "Формат: /%s <id пользователя>"
	textUsageAllow      = "Формат: /allow_worker <id> <fingerprint>"
	textUsageRevoke     = "Формат: /revoke_worker <id>"
	textWorkerAllowed   = "Воркер %s разрешён."
	textWorkerRevoked   = "Воркер %s отозван."
	textNoSuchWorker    = "Нет воркера %s."
	textCookiesHowTo    = "Пришлите файл cookies.txt с подписью /cookies."
	textCookiesTooBig   = "Файл больше 64 КБ — это не cookies.txt."
	textCookiesSaved    = "Cookies Instagram обновлены и отправлены воркерам."
	textCookiesBad      = "Не похоже на cookies.txt Instagram: %s"
	textCookiesNoWorker = "Не удалось скачать файл: нет воркера на связи. Пришлите его ещё раз позже."
	textFailed          = "Не получилось: %s"
)
