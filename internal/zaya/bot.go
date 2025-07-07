package zaya

import (
	"fmt"
	"io"
	"math"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
)

type Bot struct {
	bot *tele.Bot
	ai  *AI
	db  *DB
	ce  *ContentExtractor
	ac  *AlbumCache
	wlc string
	adm int64
	acc Accept
	mxs int64
	log *zap.SugaredLogger

	continueMenu *tele.ReplyMarkup

	startedAt   time.Time
	aiMSgCount  atomic.Int64
	aiMsgLength atomic.Int64
	aiHstLength atomic.Int64
}

func NewBot(cfg Config, ai *AI, db *DB) (*Bot, bool) {
	bot := &Bot{
		ai:        ai,
		db:        db,
		ce:        NewContentExtractor(),
		wlc:       cfg.Welcome,
		adm:       cfg.AdminID,
		acc:       cfg.Ai.Accept,
		mxs:       int64(cfg.Ai.MaxSize),
		log:       zap.L().Named("bot").Sugar(),
		startedAt: time.Now(),
	}

	bot.ac = NewAlbumCache(bot.processMessages)
	bot.ce.SetMaxDownloadSize(bot.mxs)

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
	bot.bot.Handle("/get_model", bot.getCurrentModel)
	bot.bot.Handle("/stat", bot.getBotStat)
	bot.bot.Handle("/notify", bot.notifyUsers)
	bot.bot.Handle(tele.OnAddedToGroup, bot.welcome)
	bot.bot.Handle(tele.OnText, bot.readMessage)

	if bot.acc.Images {
		bot.bot.Handle(tele.OnPhoto, bot.readMessage)
	}
	if bot.acc.Audio {
		bot.bot.Handle(tele.OnVoice, bot.readMessage)
	}
	if bot.acc.Video {
		bot.bot.Handle(tele.OnVideo, bot.readMessage)
		bot.bot.Handle(tele.OnVideoNote, bot.readMessage)
	}

	return bot, true
}

func (bot *Bot) Start() {
	// Start periodic cleanup of old album timers
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			bot.ac.CleanOld(30 * time.Second)
		}
	}()

	go func() {
		bot.log.Info("starting bot")
		bot.bot.Start()
		bot.log.Info("bot stopped")
	}()
}

func (bot *Bot) Stop() {
	bot.ac.CleanAll()
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
		"len", len(c.Text()),
		"has_photo", c.Message().Photo != nil,
		"has_voice", c.Message().Voice != nil,
		"has_video", c.Message().Video != nil || c.Message().VideoNote != nil,
		"album_id", c.Message().AlbumID,
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
		"To check which model I'm currently using, send /get_model.\n" +
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

func (bot *Bot) shouldReplyTo(c tele.Context) (bool, bool, bool) {
	if len(c.Text()) > 0 && c.Text()[0] == '/' {
		return false, false, false
	}

	if len(c.Text()) == 0 &&
		c.Message().Photo == nil &&
		c.Message().Voice == nil &&
		c.Message().Video == nil &&
		c.Message().VideoNote == nil {
		return false, false, false
	}

	if c.Chat().Type == tele.ChatPrivate {
		return true, true, false
	}

	if len(c.Text()) > bot.acc.TextLen {
		return false, false, false
	}

	if c.Message().ReplyTo != nil &&
		c.Message().ReplyTo.Sender.ID == bot.bot.Me.ID {
		return true, true, false
	}

	mention := "@" + bot.bot.Me.Username
	if strings.Contains(c.Text(), mention) {
		return true, false, true
	}

	cfg := bot.db.LoadChatConfig(c.Chat().ID)
	if strings.Contains(strings.ToLower(c.Text()), cfg.Nickname) {
		return true, false, true
	}

	if c.Message().Photo != nil ||
		c.Message().Voice != nil ||
		c.Message().Video != nil ||
		c.Message().VideoNote != nil {
		return false, false, false
	}

	if rand.Intn(100) < cfg.Freq {
		return true, cfg.Freq == 100, false
	}

	return false, false, false
}

func (bot *Bot) loadPhoto(msg *tele.Message) (Image, bool) {
	if !bot.acc.Images || msg.Photo == nil {
		return Image{}, false
	}

	if msg.Photo.FileSize > bot.mxs {
		return Image{}, false
	}

	rc, err := bot.bot.File(&msg.Photo.File)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Image{}, false
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Image{}, false
	}

	return Image{
		Data:    data,
		Caption: msg.Photo.Caption,
		Height:  msg.Photo.Height,
		Width:   msg.Photo.Width,
	}, true
}

func (bot *Bot) loadVoice(msg *tele.Message) (Audio, bool) {
	if !bot.acc.Audio || msg.Voice == nil {
		return Audio{}, false
	}

	if msg.Voice.FileSize > bot.mxs || msg.Voice.Duration > bot.acc.AudioLen {
		return Audio{}, false
	}

	rc, err := bot.bot.File(&msg.Voice.File)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Audio{}, false
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Audio{}, false
	}

	return Audio{
		Data:     data,
		Caption:  msg.Voice.Caption,
		Duration: msg.Voice.Duration,
	}, true
}

func (bot *Bot) loadVideoNote(msg *tele.Message) (Video, bool) {
	if !bot.acc.Video || msg.VideoNote == nil {
		return Video{}, false
	}

	if msg.VideoNote.FileSize > bot.mxs || msg.VideoNote.Duration > bot.acc.VideoLen {
		return Video{}, false
	}

	rc, err := bot.bot.File(&msg.VideoNote.File)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Video{}, false
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Video{}, false
	}

	return Video{
		Data:     data,
		Duration: msg.VideoNote.Duration,
		Width:    384,
		Height:   384,
	}, true
}

func (bot *Bot) loadVideo(msg *tele.Message) (Video, bool) {
	if !bot.acc.Video || msg.Video == nil {
		return Video{}, false
	}

	if msg.Video.FileSize > bot.mxs || msg.Video.Duration > bot.acc.VideoLen {
		return Video{}, false
	}

	rc, err := bot.bot.File(&msg.Video.File)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Video{}, false
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
		return Video{}, false
	}

	return Video{
		Data:     data,
		Caption:  msg.Video.Caption,
		Duration: msg.Video.Duration,
		Width:    msg.Video.Width,
		Height:   msg.Video.Height,
	}, true
}

func (bot *Bot) loadPages(msg *tele.Message) ([]string, bool) {
	var pages []string

	for _, e := range msg.Entities {
		if e.Type == tele.EntityURL {
			url := e.URL
			if url == "" {
				start, end := UTF16OffsetToUTF8(msg.Text, e.Offset, e.Length)
				url = msg.Text[start:end]
			}

			bot.log.Infow("loading page", "url", url)
			page, err := bot.ce.ExtractContent(url)
			if err != nil {
				bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
				continue
			}

			pages = append(pages, page)
		}
	}

	return pages, len(pages) > 0
}

func (bot *Bot) getAiReply(msgs []*tele.Message, userMsgs []string, isReply bool) (AIReply, bool) {
	req := NewAIRequest(msgs[0].Chat.ID, isReply)
	req.Messages = userMsgs

	var size int64
	for _, msg := range msgs {
		if img, ok := bot.loadPhoto(msg); ok {
			req.Images = append(req.Images, img)
			size += int64(len(img.Data))
		}

		if audio, ok := bot.loadVoice(msg); ok {
			req.Audios = append(req.Audios, audio)
			size += int64(len(audio.Data))
		}

		if pages, ok := bot.loadPages(msg); ok {
			req.Docs = append(req.Docs, pages...)
			for _, page := range pages {
				size += int64(len(page))
			}
		}

		if video, ok := bot.loadVideo(msg); ok {
			req.Videos = append(req.Videos, video)
			size += int64(len(video.Data))
		}

		if videoNote, ok := bot.loadVideoNote(msg); ok {
			req.Videos = append(req.Videos, videoNote)
			size += int64(len(videoNote.Data))
		}

		if size > bot.mxs {
			bot.log.Warnw("request size exceeded", "chat_id", msg.Chat.ID, "size", size)
			return AIReply{}, false
		}
	}

	return bot.ai.GetReply(req)
}

func (bot *Bot) sendAiReply(msgs []*tele.Message, userMsgs []string, isReply bool) error {
	msg := msgs[0]
	err := bot.bot.Notify(msg.Chat, tele.Typing)
	if err != nil {
		bot.log.Warnw(err.Error(), "chat_id", msg.Chat.ID)
	}

	ch := make(chan AIReply)
	defer close(ch)

	ticker := time.NewTicker(time.Second * 3)
	defer ticker.Stop()

	go func() {
		reply, ok := bot.getAiReply(msgs, userMsgs, isReply)
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

// TextFlags represents formatting states in markdown text
type TextFlags uint8

const (
	FlagNone        TextFlags = 0
	FlagTripleQuote TextFlags = 1 << iota
	FlagBackQuote
	FlagBold
)

func (bot *Bot) prepareMessageTextWithActions(s string) ([]string, *tele.ReplyMarkup) {
	var keyboard *tele.ReplyMarkup

	re := regexp.MustCompile(`<zaya_action>(.+?)</zaya_action>`)
	matches := re.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return prepareMessageText(s), nil
	}

	var actions []struct {
		description string
		fullMatch   string
	}

	for _, match := range matches {
		if len(match) >= 2 {
			actions = append(actions, struct {
				description string
				fullMatch   string
			}{
				description: strings.TrimSpace(match[1]),
				fullMatch:   match[0],
			})
		}
	}

	cleanedText := s
	for i, action := range actions {
		replacement := fmt.Sprintf("%d. %s", i+1, action.description)
		cleanedText = strings.ReplaceAll(cleanedText, action.fullMatch, replacement)
	}

	if len(actions) > 0 {
		keyboard = &tele.ReplyMarkup{}
		var buttons []tele.Btn
		for i, action := range actions {
			btn := keyboard.QueryChat(fmt.Sprintf("%d", i+1), action.description)
			buttons = append(buttons, btn)
		}
		keyboard.Inline(keyboard.Row(buttons...))
	}

	chunks := prepareMessageText(cleanedText)
	return chunks, keyboard
}

func prepareMessageText(s string) []string {
	const maxChunkSize = 4000

	var chunks []string
	var currentChunk strings.Builder
	currentChunk.Grow(maxChunkSize + 100) // Extra space for tags

	var currentFlags TextFlags
	var lastNewlineInBuffer, lastEndInBuffer, lastSpaceInBuffer int
	var flagsAtNewline, flagsAtEnd, flagsAtSpace TextFlags

	for i := 0; i < len(s); i++ {
		c := s[i]

		if c == '`' {
			if i+2 < len(s) && s[i+1] == '`' && s[i+2] == '`' && (i == 0 || s[i-1] == '\n') {
				currentFlags ^= FlagTripleQuote
				currentChunk.WriteString("```")
				i += 2
			} else if (currentFlags&FlagTripleQuote) == 0 && (i == 0 || s[i-1] != '\\') {
				currentFlags ^= FlagBackQuote
				currentChunk.WriteByte(c)
			} else {
				if (currentFlags&FlagTripleQuote) != 0 && s[i-1] != '\\' {
					currentChunk.WriteByte('\\')
				}
				currentChunk.WriteByte(c)
			}
		} else if c == '\\' && (currentFlags&(FlagTripleQuote|FlagBackQuote)) != 0 {
			if (i == 0 || s[i-1] != '\\') && (i+1 == len(s) || (s[i+1] != '\\' && s[i+1] != '`')) {
				currentChunk.WriteByte('\\')
			}
			currentChunk.WriteByte(c)
		} else if c == '*' && i+2 < len(s) && s[i+1] == '*' && s[i+2] == '*' && (currentFlags&(FlagTripleQuote|FlagBackQuote)) == 0 {
			currentChunk.WriteString("\\*\\*\\*")
			i += 2
		} else if c == '*' && i+1 < len(s) && s[i+1] == '*' && (currentFlags&(FlagTripleQuote|FlagBackQuote)) == 0 {
			currentFlags ^= FlagBold
			currentChunk.WriteByte('*')
			i++
		} else if (currentFlags&(FlagTripleQuote|FlagBackQuote)) == 0 && strings.ContainsRune("_^*[]()~>#+-|{}.!=", rune(c)) {
			if i == 0 || s[i-1] != '\\' {
				currentChunk.WriteByte('\\')
			}
			currentChunk.WriteByte(c)
		} else {
			currentChunk.WriteByte(c)
		}

		switch c {
		case '\n':
			lastNewlineInBuffer = currentChunk.Len()
			flagsAtNewline = currentFlags
		case '.', '!', '?':
			lastEndInBuffer = currentChunk.Len()
			flagsAtEnd = currentFlags
		case ' ':
			lastSpaceInBuffer = currentChunk.Len()
			flagsAtSpace = currentFlags
		}

		if currentChunk.Len() >= maxChunkSize {
			var splitPoint int
			var splitFlags TextFlags

			if lastNewlineInBuffer > 0 {
				splitPoint = lastNewlineInBuffer
				splitFlags = flagsAtNewline
			} else if lastEndInBuffer > 0 {
				splitPoint = lastEndInBuffer + 1
				splitFlags = flagsAtEnd
			} else if lastSpaceInBuffer > 0 {
				splitPoint = lastSpaceInBuffer
				splitFlags = flagsAtSpace
			} else {
				splitPoint = currentChunk.Len()
				splitFlags = currentFlags
			}

			bufferContent := currentChunk.String()

			leftPart := bufferContent[:splitPoint]
			rightPart := bufferContent[splitPoint:]

			var leftChunk strings.Builder
			leftChunk.WriteString(leftPart)
			if (splitFlags & FlagBold) != 0 {
				leftChunk.WriteByte('*')
			} else if (splitFlags & FlagBackQuote) != 0 {
				leftChunk.WriteByte('`')
			} else if (splitFlags & FlagTripleQuote) != 0 {
				leftChunk.WriteString("\n```")
			}

			chunks = append(chunks, leftChunk.String())

			currentChunk.Reset()
			currentChunk.Grow(maxChunkSize + 100)

			if (splitFlags & FlagTripleQuote) != 0 {
				currentChunk.WriteString("```\n")
			} else if (splitFlags & FlagBackQuote) != 0 {
				currentChunk.WriteByte('`')
			} else if (splitFlags & FlagBold) != 0 {
				currentChunk.WriteByte('*')
			}

			currentChunk.WriteString(rightPart)

			lastNewlineInBuffer = 0
			lastEndInBuffer = 0
			lastSpaceInBuffer = 0
			flagsAtNewline = FlagNone
			flagsAtEnd = FlagNone
			flagsAtSpace = FlagNone
		}
	}

	if (currentFlags & FlagTripleQuote) != 0 {
		currentChunk.WriteString("\n```")
	} else if (currentFlags & FlagBackQuote) != 0 {
		currentChunk.WriteByte('`')
	} else if (currentFlags & FlagBold) != 0 {
		currentChunk.WriteByte('*')
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	if len(chunks) == 0 {
		return []string{s}
	}

	return chunks
}

func (bot *Bot) sendReply(msg *tele.Message, reply AIReply) error {
	if reply.Text == "" {
		return nil
	}

	bot.aiMSgCount.Add(1)
	bot.aiMsgLength.Add(int64(reply.ReplyLen))
	bot.aiHstLength.Add(int64(reply.CtxLen))

	escapedChunks, actionKeyboard := bot.prepareMessageTextWithActions(reply.Text)
	prevMsg := msg

	var err error
	for i, chunk := range escapedChunks {
		isLast := i == len(escapedChunks)-1
		curMsg := prevMsg

		if !isLast || reply.AtEnd {
			if isLast && actionKeyboard != nil {
				curMsg, err = bot.bot.Reply(prevMsg, chunk, actionKeyboard, tele.ModeMarkdownV2)
			} else {
				curMsg, err = bot.bot.Reply(prevMsg, chunk, tele.ModeMarkdownV2)
			}
		} else {
			if actionKeyboard != nil {
				curMsg, err = bot.bot.Reply(prevMsg, chunk, actionKeyboard, bot.continueMenu, tele.ModeMarkdownV2)
			} else {
				curMsg, err = bot.bot.Reply(prevMsg, chunk, bot.continueMenu, tele.ModeMarkdownV2)
			}
		}

		if err != nil {
			bot.log.Warnw("error", "err", err, "text", chunk)

			if !isLast || reply.AtEnd {
				if isLast && actionKeyboard != nil {
					curMsg, err = bot.bot.Reply(prevMsg, chunk, actionKeyboard, tele.ModeDefault)
				} else {
					curMsg, err = bot.bot.Reply(prevMsg, chunk, tele.ModeDefault)
				}
			} else {
				if actionKeyboard != nil {
					curMsg, err = bot.bot.Reply(prevMsg, chunk, actionKeyboard, bot.continueMenu, tele.ModeDefault)
				} else {
					curMsg, err = bot.bot.Reply(prevMsg, chunk, bot.continueMenu, tele.ModeDefault)
				}
			}
		}

		if err != nil {
			return err
		}
		if curMsg != nil {
			prevMsg = curMsg
		}
	}

	return nil
}

func (bot *Bot) welcome(c tele.Context) error {
	err := bot.sendHelp(c)
	if err != nil {
		return err
	}

	bot.startChat(c)
	return bot.sendAiReply([]*tele.Message{c.Message()}, []string{bot.wlc}, true)
}

func (bot *Bot) readMessage(c tele.Context) error {
	shouldReply, forceKeepHistory, mentioned := bot.shouldReplyTo(c)
	if !shouldReply {
		return nil
	}

	if c.Message().AlbumID != "" {
		bot.log.Infow("album message",
			"chat_id", c.Chat().ID,
			"album_id", c.Message().AlbumID)
		bot.ac.AddMessage(c, forceKeepHistory, mentioned)
		return nil
	}

	return bot.processMessages([]tele.Context{c}, forceKeepHistory, mentioned)
}

func (bot *Bot) processMessages(contexts []tele.Context, forceKeepHistory, mentioned bool) error {
	if len(contexts) == 0 {
		return nil
	}

	beginTime := time.Now().UnixNano()

	c := contexts[0]
	msg := c.Message()
	text := c.Text()
	text = strings.ReplaceAll(text, "@"+bot.bot.Me.Username, "")
	text = strings.TrimSpace(text)

	msgTexts := []string{}
	if text != "" {
		msgTexts = append(msgTexts, text)
	}

	if msg.ReplyTo != nil &&
		mentioned &&
		(msg.ReplyTo.Text != "" ||
			msg.ReplyTo.Photo != nil ||
			msg.ReplyTo.Voice != nil ||
			msg.ReplyTo.Video != nil ||
			msg.ReplyTo.VideoNote != nil) {
		if msg.ReplyTo.Text != "" {
			msgTexts = append(msgTexts, msg.ReplyTo.Text)
		}
		msg = msg.ReplyTo
	}

	msgs := []*tele.Message{msg}
	for i := 1; i < len(contexts); i++ {
		msgs = append(msgs, contexts[i].Message())
	}

	if !bot.ai.IsChatStarted(c.Chat().ID) {
		bot.startChat(c)
	}

	err := bot.sendAiReply(msgs, msgTexts, forceKeepHistory)

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
		err = bot.sendAiReply([]*tele.Message{c.Message()}, []string{"continue"}, true)
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

func (bot *Bot) getCurrentModel(c tele.Context) error {
	const msgTxt = "" +
		"I am currently utilizing the <b>%s</b> model.\n" +
		"The primary model is a more recent and sophisticated iteration of the LLM, " +
		"albeit with certain usage limits. Occasionally, in the event of exceeding " +
		"the allocated quota, I can seamlessly transition to the auxiliary model. " +
		"It is essential to note that these limitations are universally applied, " +
		"affecting all users collectively rather than individually. " +
		"While the auxiliary model may not possess the same level of capabilities " +
		"as its primary counterpart, it is more than adequate for the vast majority of tasks."

	model := "primary"
	if bot.ai.IsAltModel() {
		model = "auxiliary"
	}

	msg := fmt.Sprintf(msgTxt, model)
	return c.Reply(msg, tele.ModeHTML)
}

func (bot *Bot) notifyUsers(c tele.Context) error {
	if c.Sender().ID != bot.adm {
		return nil
	}

	text := c.Message().Text
	idx := strings.IndexByte(text, ' ') + 1
	if idx > 0 {
		text = text[idx:]
		text = strings.TrimSpace(text)
	}
	if idx == 0 || len(text) == 0 {
		const errStr = "Example usage: `/notify Notification message.`."
		return c.Reply(errStr, tele.ModeMarkdown)
	}

	sentCnt := 0
	chatIDs := bot.db.LoadAllChatIDs()
	for _, chatID := range chatIDs {
		_, err := bot.bot.Send(tele.ChatID(chatID), text, tele.ModeMarkdown)
		if err != nil {
			bot.log.Warnw("error", "chat", chatID, "err", err)
		} else {
			sentCnt++
		}
	}

	bot.log.Infow("sent notifications", "chats", sentCnt)

	return nil
}

func (bot *Bot) getBotStat(c tele.Context) error {
	var msg strings.Builder

	addF64 := func(title string, value float64) {
		if math.IsInf(value, 0) || math.IsNaN(value) {
			value = 0
		}

		msg.WriteString(fmt.Sprintf("%s: %.2f\n", title, value))
	}

	addI64 := func(title string, value int64) {
		msg.WriteString(fmt.Sprintf("%s: %d\n", title, value))
	}

	uptimeDays := time.Since(bot.startedAt).Hours() / 24
	addF64("Uptime (days)", uptimeDays)

	totalMsgCnt := bot.aiMSgCount.Load()
	addI64("Total count of output messages", totalMsgCnt)

	totalMsgLen := bot.aiMsgLength.Load()
	addI64("Total length of output messages (KiB)", totalMsgLen/1024)

	if uptimeDays > 0.1 {
		avgMsgCnt := float64(totalMsgCnt) / uptimeDays
		addF64("Count of output messages per day", avgMsgCnt)

		avgMsgLen := float64(totalMsgLen) / 1024 / uptimeDays
		addF64("Length of output messages per day (KiB)", avgMsgLen)

		avgHstLen := float64(bot.aiHstLength.Load()) / 1024 / uptimeDays
		addF64("Length of input context per day (KiB)", avgHstLen)
	}

	groupChatCnt := bot.db.GetChatCount()
	addI64("Total count of chats", groupChatCnt)

	customRoleCnt := bot.db.GetCustomRoleCount()
	addI64("Count of custom roles", customRoleCnt)

	return c.Reply(msg.String(), tele.ModeHTML)
}
