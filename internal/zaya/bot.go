package zaya

import (
	"fmt"
	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
	"math/rand"
	"strconv"
	"strings"
	"time"
)

type Bot struct {
	bot *tele.Bot
	ai  *AI
	db  *DB
	wlc string
	log *zap.SugaredLogger

	continueMenu *tele.ReplyMarkup
}

func NewBot(cfg Config, ai *AI, db *DB) (*Bot, bool) {
	bot := &Bot{
		ai:  ai,
		db:  db,
		wlc: cfg.Welcome,
		log: zap.L().Named("bot").Sugar(),
	}

	pref := tele.Settings{
		Token:   cfg.TgToken,
		Poller:  &tele.LongPoller{Timeout: 30 * time.Second},
		OnError: bot.logError,
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		bot.log.Error(err)
		return nil, false
	}
	bot.bot = b

	{
		menu := &tele.ReplyMarkup{}
		btn := menu.Data("⇒", "continue")
		bot.bot.Handle(&btn, bot.continueAiReply)
		menu.Inline(menu.Row(btn))
		bot.continueMenu = menu
	}

	bot.bot.Use(middleware.Recover())
	bot.bot.Use(bot.logCmd)

	bot.bot.Handle("/restart_chat", bot.restartChat)
	bot.bot.Handle("/get_frequency", bot.getFrequency)
	bot.bot.Handle("/set_frequency", bot.setFrequency)
	bot.bot.Handle("/get_prompt", bot.getSystemPrompt)
	bot.bot.Handle("/set_prompt", bot.setSystemPrompt)
	bot.bot.Handle("/get_nickname", bot.getNickname)
	bot.bot.Handle("/set_nickname", bot.setNickname)
	bot.bot.Handle("/get_max_history", bot.getMaxHistory)
	bot.bot.Handle("/set_max_history", bot.setMaxHistory)
	bot.bot.Handle("/select_role", bot.selectRole)
	bot.bot.Handle("/remove_role", bot.selectRemoveRole)
	bot.bot.Handle("/save_role", bot.saveRole)
	bot.bot.Handle("/help", bot.sendHelp)
	bot.bot.Handle("/start", bot.welcome)
	bot.bot.Handle(tele.OnAddedToGroup, bot.welcome)
	bot.bot.Handle(tele.OnText, bot.readMessage)

	return bot, true
}

func (bot *Bot) Start() {
	go func() {
		bot.log.Info("starting bot")
		bot.bot.Start()
		bot.log.Info("bot stopped")
	}()
}

func (bot *Bot) Stop() {
	bot.bot.Stop()
}

func (bot *Bot) logMessage(c tele.Context, beginTime int64, err error) {
	endTime := time.Now().UnixNano()
	duration := float64(endTime-beginTime) / 1000000

	isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
	var cmd string
	if isCmd {
		cmd = c.Text()
	}
	bot.log.Infow("user message",
		"chat_id", c.Chat().ID,
		"chat_type", c.Chat().Type,
		"user_id", c.Sender().ID,
		"user_name", c.Sender().Username,
		"is_cmd", isCmd,
		"cmd", cmd,
		"size", len(c.Text()),
		"dur", fmt.Sprintf("%.2f", duration),
		"err", err)
}

func (bot *Bot) logCmd(next tele.HandlerFunc) tele.HandlerFunc {
	mention := "@" + bot.bot.Me.Username

	return func(c tele.Context) error {
		beginTime := time.Now().UnixNano()
		isBotCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1 &&
			(c.Chat().Type == tele.ChatPrivate ||
				strings.Contains(c.Text(), mention) ||
				!strings.Contains(c.Text(), "@"))

		err := next(c)

		if isBotCmd {
			bot.logMessage(c, beginTime, err)
		}

		return err
	}
}

func (bot *Bot) logError(err error, c tele.Context) {
	if c == nil {
		bot.log.Errorw("error", "err", err)
	} else {
		isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
		var cmd string
		if isCmd {
			cmd = c.Text()
			idx := strings.Index(cmd, " ")
			if idx > 0 {
				cmd = cmd[:idx]
			}
		}
		bot.log.Errorw("error",
			"chat_id", c.Chat().ID,
			"chat_type", c.Chat().Type,
			"user_id", c.Sender().ID,
			"user_name", c.Sender().Username,
			"is_cmd", isCmd,
			"cmd", cmd,
			"size", len(c.Text()),
			"err", err)
	}
}

func (bot *Bot) sendHelp(c tele.Context) error {
	const text = "" +
		"Greetings! I'm a sophisticated AI-powered bot, capable of assisting you with " +
		"a multitude of tasks or engaging in captivating conversations. Feel free " +
		"to converse with me, pose questions, and I'll respond with insightful answers.\n\n" +
		"You're welcome to initiate a private chat with me, and I'll respond to all your messages.\n\n" +
		"In group chats, I'll respond when you address me directly (using @), " +
		"refer to me by my nickname (accessible via /get_nickname), " +
		"or reply to one of my previous messages. If you mention me in response to another message, " +
		"I'll address that specific message instead. Additionally, I may respond to a percentage " +
		"of random messages to maintain a lively conversation (configurable via /set_frequency).\n\n" +
		"The most interesting command at your disposal is /select_role. " +
		"I encourage you to explore its possibilities.\n\n" +
		"To customize your experience, utilize the following commands:\n\n" +
		"To use a predefined or created persona, send /select_role, which comprises " +
		"a system prompt, a nickname, and a history limit.\n" +
		"To preserve the current persona for future use, send /save_role.\n" +
		"To discard an unused persona, send /remove_role.\n" +
		"To reboot our conversation, send /restart_chat, and I'll forget our previous messages.\n" +
		"To access my current system instructions, send /get_prompt.\n" +
		"To update these instructions, send /set_prompt.\n" +
		"To view the number of messages I'll attempt to keep in my mind, send /get_max_history.\n" +
		"To modify this number, send /set_max_history.\n" +
		"To discover how to address me in group chats, send /get_nickname.\n" +
		"To alter my nickname, send /set_nickname.\n" +
		"To see how frequently I'll respond to random messages in group chats, send /get_frequency.\n" +
		"To adjust this setting, send /set_frequency.\n" +
		"To revisit this guidance, send /help."

	return c.Reply(text)
}

func (bot *Bot) startChat(c tele.Context) {
	cfg := bot.db.LoadChatConfig(c.Chat().ID)
	bot.ai.StartChat(c.Chat().ID, cfg.Prompt, cfg.MaxHistory)
}

func (bot *Bot) restartChat(c tele.Context) error {
	bot.startChat(c)
	return c.Reply("Chat history cleared.")
}

func (bot *Bot) getFrequency(c tele.Context) error {
	freq := bot.db.LoadChatConfig(c.Chat().ID).Freq
	text := fmt.Sprintf("I will respond to a percentage of %d%% random messages in the chat.", freq)
	return c.Reply(text)
}

func (bot *Bot) setFrequency(c tele.Context) error {
	const errStr = "" +
		"Example usage: `/set_frequency 10`.\n" +
		"Set the frequency to 0, and I will remain dormant, " +
		"only responding when directly addressed in the group chat.\n" +
		"Set the frequency to 50, and I will respond to approximately " +
		"half of all messages in the group chat.\n" +
		"Set the frequency to 100, and I will engage " +
		"with every message in the group chat."

	args := c.Args()
	if len(args) != 1 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	freq, err := strconv.Atoi(args[0])
	if err != nil || freq < 0 || freq > 100 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	bot.db.SetFreq(c.Chat().ID, freq)
	return c.Reply("Frequency changed.")
}

func (bot *Bot) getSystemPrompt(c tele.Context) error {
	prompt := bot.db.LoadChatConfig(c.Chat().ID).Prompt
	text := "Current system prompt:\n```\n" + prompt + "\n```"
	return c.Reply(text, tele.ModeMarkdown)
}

func (bot *Bot) setSystemPrompt(c tele.Context) error {
	const errStr = "Example usage: `/set_prompt You are a helpful assistant`."

	text := c.Text()
	idx := len("/set_prompt ")
	if len(text) <= idx {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	text = text[idx:]
	text = strings.TrimSpace(text)
	if len(text) == 0 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	bot.db.SetPrompt(c.Chat().ID, text)
	bot.startChat(c)

	return c.Reply("System prompt changed.")
}

func (bot *Bot) getNickname(c tele.Context) error {
	nickname := bot.db.LoadChatConfig(c.Chat().ID).Nickname
	text := fmt.Sprintf("You can call me %s.", nickname)
	return c.Reply(text)
}

func (bot *Bot) setNickname(c tele.Context) error {
	const errStr = "" +
		"Example usage: `/set_nickname llama`.\n" +
		"You can call me by this name."

	args := c.Args()
	if len(args) != 1 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	nickname := strings.ToLower(args[0])
	bot.db.SetNickname(c.Chat().ID, nickname)
	return c.Reply("Nickname changed.")
}

func (bot *Bot) getMaxHistory(c tele.Context) error {
	maxHistory := bot.db.LoadChatConfig(c.Chat().ID).MaxHistory
	if maxHistory > 0 {
		const maxText = "" +
			"My conversational memory will be " +
			"capped at max %d preceding messages."

		text := fmt.Sprintf(maxText, maxHistory)
		return c.Reply(text)
	}

	const zeroText = "" +
		"My conversational memory will be unlimited, " +
		"retaining all preceding messages within my capabilities."

	return c.Reply(zeroText)
}

func (bot *Bot) setMaxHistory(c tele.Context) error {
	const errStr = "" +
		"Example usage: `/set_max_history 10`.\n" +
		"Set a limit of 0, and I will attempt to retain " +
		"as many preceding messages as possible within the context window.\n" +
		"Set a positive numerical limit, and I will purge older messages " +
		"when the designated threshold is surpassed or " +
		"the context window capacity is exceeded, whichever occurs first."

	args := c.Args()
	if len(args) != 1 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	limit, err := strconv.Atoi(args[0])
	if err != nil || limit < 0 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	bot.db.SetMaxHistory(c.Chat().ID, limit)
	return c.Reply("History limit changed.")
}

func (bot *Bot) shouldReplyTo(c tele.Context) (bool, bool) {
	if len(c.Text()) == 0 || c.Text()[0] == '/' {
		return false, false
	}

	if c.Chat().Type == tele.ChatPrivate {
		return true, true
	}

	if len(c.Text()) > 1000 {
		return false, false
	}

	if c.Message().ReplyTo != nil &&
		c.Message().ReplyTo.Sender.ID == bot.bot.Me.ID {
		return true, true
	}

	mention := "@" + bot.bot.Me.Username
	if strings.Contains(c.Text(), mention) {
		return true, false
	}

	cfg := bot.db.LoadChatConfig(c.Chat().ID)
	if strings.Contains(strings.ToLower(c.Text()), cfg.Nickname) {
		return true, false
	}

	if rand.Intn(100) < cfg.Freq {
		return true, cfg.Freq == 100
	}

	return false, false
}

func (bot *Bot) sendAiReply(msg *tele.Message, userMsg string, isReply bool) error {
	err := bot.bot.Notify(msg.Chat, tele.Typing)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
	}

	ch := make(chan AIReply)
	defer close(ch)

	ticker := time.NewTicker(time.Second * 3)
	defer ticker.Stop()

	go func() {
		reply, ok := bot.ai.GetReply(msg.Chat.ID, userMsg, isReply)
		if ok {
			ch <- reply
		} else {
			ch <- AIReply{}
		}
	}()

	for {
		select {
		case <-ticker.C:
			err = bot.bot.Notify(msg.Chat, tele.Typing)
			if err != nil {
				bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
			}
		case reply := <-ch:
			return bot.sendReply(msg, reply)
		}
	}
}

func escapeSpecialChars(s string) string {
	var result strings.Builder
	result.Grow(len(s))

	inTripleQuote := false
	inBackQuote := false
	isBoldText := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '`' {
			if i+2 < len(s) && s[i+1] == '`' && s[i+2] == '`' && i > 0 && s[i-1] == '\n' {
				inTripleQuote = !inTripleQuote
				result.WriteString("```")
				i += 2
			} else if !inTripleQuote && (i == 0 || s[i-1] != '\\') {
				inBackQuote = !inBackQuote
				result.WriteByte(c)
			} else {
				if inTripleQuote && s[i-1] != '\\' {
					result.WriteByte('\\')
				}
				result.WriteByte(c)
			}
		} else if c == '\\' && (inTripleQuote || inBackQuote) {
			if (i == 0 || s[i-1] != '\\') && (i+1 == len(s) || (s[i+1] != '\\' && s[i+1] != '`')) {
				result.WriteByte('\\')
			}
			result.WriteByte(c)
		} else if c == '*' && i+1 < len(s) && s[i+1] == '*' && !inTripleQuote && !inBackQuote {
			isBoldText = !isBoldText
			result.WriteString("*")
			i++
		} else if !inTripleQuote && !inBackQuote && strings.ContainsRune("_^*[]()~>#+-|{}.!=", rune(c)) {
			if i == 0 || s[i-1] != '\\' {
				result.WriteByte('\\')
			}
			result.WriteByte(c)
		} else {
			result.WriteByte(c)
		}
	}

	if inTripleQuote {
		result.WriteString("\n```")
	} else if inBackQuote {
		result.WriteByte('`')
	} else if isBoldText {
		result.WriteString("**")
	}

	return result.String()
}

func (bot *Bot) sendReply(msg *tele.Message, reply AIReply) error {
	if reply.Text == "" {
		return nil
	}

	escapedText := escapeSpecialChars(reply.Text)

	var err error
	if reply.AtEnd {
		_, err = bot.bot.Reply(msg, escapedText, tele.ModeMarkdownV2)
	} else {
		_, err = bot.bot.Reply(msg, escapedText, bot.continueMenu, tele.ModeMarkdownV2)
	}

	if err != nil {
		bot.log.Warnw("error", "err", err, "text", reply.Text)

		if reply.AtEnd {
			_, err = bot.bot.Reply(msg, reply.Text, tele.ModeDefault)
		} else {
			_, err = bot.bot.Reply(msg, reply.Text, bot.continueMenu, tele.ModeDefault)
		}
	}

	return err
}

func (bot *Bot) welcome(c tele.Context) error {
	err := bot.sendHelp(c)
	if err != nil {
		return err
	}

	bot.startChat(c)
	return bot.sendAiReply(c.Message(), bot.wlc, true)
}

func (bot *Bot) readMessage(c tele.Context) error {
	beginTime := time.Now().UnixNano()

	shouldReply, forceKeepHistory := bot.shouldReplyTo(c)
	if !shouldReply {
		return nil
	}

	mention := "@" + bot.bot.Me.Username
	msg := c.Message()
	text := c.Text()
	if msg.ReplyTo != nil && msg.ReplyTo.Text != "" &&
		msg.Sender.ID != bot.bot.Me.ID &&
		strings.Contains(text, mention) {
		msg = msg.ReplyTo
		text = msg.Text
	}
	text = strings.ReplaceAll(text, mention, "")
	text = strings.TrimSpace(text)

	if !bot.ai.IsChatStarted(c.Chat().ID) {
		bot.startChat(c)
	}

	err := bot.sendAiReply(msg, text, forceKeepHistory)

	bot.logMessage(c, beginTime, err)

	return err
}

func (bot *Bot) continueAiReply(c tele.Context) error {
	beginTime := time.Now().UnixNano()

	err := c.Edit(&tele.ReplyMarkup{})
	if err != nil {
		return err
	}

	if bot.ai.IsChatStarted(c.Chat().ID) {
		err = bot.sendAiReply(c.Message(), "continue", true)
	}

	bot.logMessage(c, beginTime, err)

	return err
}

func (bot *Bot) createRoleMenu(roles []BotRole, unique string, handler tele.HandlerFunc) *tele.ReplyMarkup {
	roleMenu := &tele.ReplyMarkup{}
	roleBtns := make([]tele.Btn, 0)
	for _, role := range roles {

		btn := roleMenu.Data(role.Name, unique, fmt.Sprintf("%d", role.ID))
		roleBtns = append(roleBtns, btn)

		bot.bot.Handle(&btn, handler)
	}
	rows := roleMenu.Split(3, roleBtns)
	roleMenu.Inline(rows...)

	return roleMenu
}

func (bot *Bot) selectRole(c tele.Context) error {
	roles := bot.db.LoadAllRoleNames(c.Chat().ID)
	roleMenu := bot.createRoleMenu(roles, "set_role", bot.setRole)
	const text = "" +
		"Select a role, which will subsequently establish a system prompt, " +
		"assign a nickname, and determine a history limit. " +
		"Upon selection, the chat will be restarted."

	return c.Reply(text, roleMenu)
}

func (bot *Bot) setRole(c tele.Context) error {
	id, err := strconv.Atoi(c.Args()[0])
	if err != nil {
		return err
	}

	role, ok := bot.db.SetRole(c.Chat().ID, uint(id))
	bot.startChat(c)
	if ok {
		text := fmt.Sprintf("Now I'm acting as *%s*.", role.Name)
		if role.Example != "" {
			text += "\nTry send this message: _" + role.Example + "_"
		}

		err = c.Edit(text, tele.ModeMarkdown)
	} else {
		err = c.Edit("Can't select this role.")
	}
	if err != nil {
		return err
	}

	return c.Respond()
}

func (bot *Bot) selectRemoveRole(c tele.Context) error {
	text := "" +
		"Select a role to remove. But note that only roles " +
		"created within this Telegram chat may be removed."

	roles := bot.db.LoadChatRoleNames(c.Chat().ID)
	if len(roles) > 0 {
		roleMenu := bot.createRoleMenu(roles, "remove_role", bot.removeRole)
		return c.Reply(text, roleMenu)
	}

	text += "\n(no roles)"
	return c.Reply(text)
}

func (bot *Bot) removeRole(c tele.Context) error {
	id, err := strconv.Atoi(c.Args()[0])
	if err != nil {
		return err
	}

	ok := bot.db.RemoveRole(c.Chat().ID, uint(id))
	if ok {
		err = c.Edit("Role removed.")
	} else {
		err = c.Edit("Can't remove this role.")
	}
	if err != nil {
		return err
	}

	return c.Respond()
}

func (bot *Bot) saveRole(c tele.Context) error {
	const errStr = "" +
		"Example usage: `/save_role en Assistant`.\n" +
		"The first argument denotes the language, " +
		"represented by a two-letter code, " +
		"which serves as a sorting criterion.\n" +
		"The second argument specifies the new role name, " +
		"limited to a maximum of 20 characters."

	args := c.Args()
	if len(args) < 2 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	lang := c.Args()[0]
	if len(lang) != 2 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	name := strings.Join(c.Args()[1:], " ")
	if len([]rune(name)) > 20 {
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	bot.db.SaveRole(c.Chat().ID, lang, name)

	return c.Reply("Role saved.")
}
