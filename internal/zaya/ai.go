package zaya

import (
	"context"
	"fmt"
	"github.com/erni27/imcache"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/mistral"
	"github.com/tmc/langchaingo/llms/openai"
	"go.uber.org/zap"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type aiChat struct {
	messages []llms.MessageContent
	msgLens  []int
	curCtx   int
	maxCtx   int
	maxHst   int
	lastTime time.Time
	hstLock  sync.Mutex
	log      *zap.SugaredLogger
}

func newAiChat(prompt string, nCtx, maxHistory int, log *zap.SugaredLogger) *aiChat {
	chat := &aiChat{
		messages: make([]llms.MessageContent, 0, 3),
		msgLens:  make([]int, 0, 3),
		maxCtx:   nCtx,
		maxHst:   maxHistory,
		lastTime: time.Now(),
		log:      log,
	}

	chat.addMessage(llms.ChatMessageTypeSystem, prompt, 4000)

	return chat
}

func getMessageLen(text string, maxTok int) int {
	msgLen := len(text)
	if msgLen > maxTok {
		msgLen = maxTok
	}
	return msgLen
}

func (chat *aiChat) addMessage(role llms.ChatMessageType, text string, maxTok int) {
	msg := llms.MessageContent{
		Role:  role,
		Parts: make([]llms.ContentPart, 0),
	}

	part := llms.TextPart(text)
	msg.Parts = append(msg.Parts, part)
	chat.messages = append(chat.messages, msg)
	chat.lastTime = time.Now()

	msgLen := getMessageLen(text, maxTok)
	chat.msgLens = append(chat.msgLens, msgLen)
	chat.curCtx += msgLen

	if (chat.maxCtx > 0 && chat.curCtx >= chat.maxCtx) ||
		(chat.maxHst > 0 && len(chat.messages)-1 > chat.maxHst) {
		chat.cleanHistory()
	}
}

func (chat *aiChat) addUserMessage(text string) {
	chat.addMessage(llms.ChatMessageTypeHuman, text, 4000)
}

func (chat *aiChat) addBotMessage(text string, maxTok int) {
	chat.addMessage(llms.ChatMessageTypeAI, text, maxTok)
}

func (chat *aiChat) removeLastMessage() {
	chat.curCtx -= chat.msgLens[len(chat.msgLens)-1]
	chat.msgLens = chat.msgLens[:len(chat.msgLens)-1]
	chat.messages = chat.messages[:len(chat.messages)-1]
}

func (chat *aiChat) cleanHistory() {
	msgCnt := len(chat.messages) - 1
	if msgCnt == 0 {
		return
	}

	rmCnt := 0
	for rmCnt < msgCnt &&
		((chat.maxCtx > 0 && chat.curCtx >= chat.maxCtx) ||
			(chat.maxHst > 0 && msgCnt-rmCnt > chat.maxHst)) {
		rmCnt++
		chat.curCtx -= chat.msgLens[rmCnt]
	}
	for (rmCnt == 0 || rmCnt%2 != 0) && rmCnt < msgCnt {
		rmCnt++
		chat.curCtx -= chat.msgLens[rmCnt]
	}

	chat.msgLens = append(chat.msgLens[:1], chat.msgLens[rmCnt+1:]...)
	chat.messages = append(chat.messages[:1], chat.messages[rmCnt+1:]...)

	chat.log.Infow("clean history",
		"removed", rmCnt,
		"left", len(chat.messages),
		"ctx", chat.curCtx)
}

func (chat *aiChat) isExpired(maxDur time.Duration) bool {
	return time.Since(chat.lastTime) > maxDur
}

func (chat *aiChat) restart() {
	chat.curCtx = chat.msgLens[0]
	chat.msgLens = chat.msgLens[:1]
	chat.messages = chat.messages[:1]
}

type AI struct {
	llm     llms.Model
	altLlm  llms.Model
	isAlt   atomic.Bool
	chats   imcache.Cache[int64, *aiChat]
	opts    []llms.CallOption
	log     *zap.SugaredLogger
	maxCtx  int
	maxTok  int
	maxDur  time.Duration
	chatExp imcache.Expiration
}

func NewAI(cfg AiConfig) (*AI, bool) {
	ai := &AI{
		opts:   make([]llms.CallOption, 0),
		log:    zap.L().Named("ai").Sugar(),
		maxCtx: cfg.NCtx - cfg.MaxTok,
		maxTok: cfg.MaxTok,
	}

	ai.maxDur = cfg.ExpTime
	ai.chatExp = imcache.WithSlidingExpiration(cfg.ChatExp)

	ai.log.Infow("creating AI",
		"provider", cfg.Provider,
		"base_url", cfg.BaseUrl,
		"api_key_set", cfg.ApiKey != "",
		"model", cfg.Model,
		"rep_pen", cfg.RepPen,
		"top_k", cfg.TopK,
		"temperature", cfg.Temp,
		"max_tokens", cfg.MaxTok,
		"stop_words", cfg.Stop)

	var err error
	switch cfg.Provider {
	case "openai":
		opts := make([]openai.Option, 0)
		if cfg.BaseUrl != "" {
			opts = append(opts, openai.WithBaseURL(cfg.BaseUrl))
		}
		if cfg.ApiKey != "" {
			opts = append(opts, openai.WithToken(cfg.ApiKey))
		}
		if cfg.Model != "" {
			opts = append(opts, openai.WithModel(cfg.Model))
		}
		ai.llm, err = openai.New(opts...)

		opts = opts[:2]
		if cfg.AltModel != "" {
			opts = append(opts, openai.WithModel(cfg.AltModel))
		}
		ai.altLlm, err = openai.New(opts...)
	case "mistral":
		opts := make([]mistral.Option, 0)
		if cfg.BaseUrl != "" {
			opts = append(opts, mistral.WithEndpoint(cfg.BaseUrl))
		}
		if cfg.ApiKey != "" {
			opts = append(opts, mistral.WithAPIKey(cfg.ApiKey))
		}
		if cfg.Model != "" {
			opts = append(opts, mistral.WithModel(cfg.Model))
		}
		ai.llm, err = mistral.New(opts...)
	default:
		err = fmt.Errorf("unknown AI provider: %s", cfg.Provider)
	}
	if err != nil {
		ai.log.Error(err)
		return nil, false
	}

	ai.opts = append(ai.opts, llms.WithRepetitionPenalty(cfg.RepPen))
	ai.opts = append(ai.opts, llms.WithTemperature(cfg.Temp))
	ai.opts = append(ai.opts, llms.WithTopK(cfg.TopK))
	ai.opts = append(ai.opts, llms.WithMaxTokens(cfg.MaxTok))
	ai.opts = append(ai.opts, llms.WithStopWords(cfg.Stop))

	return ai, true
}

func (ai *AI) IsAltModel() bool {
	return ai.isAlt.Load()
}

func (ai *AI) IsChatStarted(chatID int64) bool {
	_, exists := ai.chats.Get(chatID)
	return exists
}

func (ai *AI) createChat(chatID int64, prompt string, maxHistory int) *aiChat {
	chat := newAiChat(prompt, ai.maxCtx, maxHistory, ai.log)
	ai.chats.Set(chatID, chat, ai.chatExp)
	return chat
}

func (ai *AI) StartChat(chatID int64, prompt string, maxHistory int) {
	ai.createChat(chatID, prompt, maxHistory)
	ai.log.Infow("chat started", "chat_id", chatID)
}

func (ai *AI) generate(chatID int64, chat *aiChat, nTry int) (*llms.ContentResponse, bool) {
	if nTry > 5 {
		return nil, false
	}

	llm := ai.llm
	isAlt := ai.isAlt.Load()
	if isAlt {
		llm = ai.altLlm
	}

	resp, err := llm.GenerateContent(context.Background(), chat.messages, ai.opts...)
	if err == nil {
		return resp, true
	}

	if strings.Contains(err.Error(), "Service Unavailable") {
		sec := nTry * 3
		ai.log.Infow("sleeping", "sec", sec)
		time.Sleep(time.Duration(sec) * time.Second)

		return ai.generate(chatID, chat, nTry+1)
	}

	idx := strings.Index(err.Error(), "Please try again in")
	if idx <= 0 {
		ai.log.Warnw(err.Error(), "chat_id", chatID)
		return nil, false
	}

	chat.cleanHistory()

	str := err.Error()[idx:]
	str = regexp.MustCompile(`\d+`).FindString(str)
	sec, err := strconv.Atoi(str)
	if err != nil {
		ai.log.Warnw(err.Error(), "chat_id", chatID)
		return nil, false
	}

	sec++
	dur := time.Duration(sec) * time.Second
	if isAlt {
		ai.log.Infow("sleeping", "sec", sec)
		time.Sleep(dur)
	} else {
		ai.log.Infow("switching to alt model", "sec", sec)
		ai.isAlt.Store(true)
		time.AfterFunc(dur, func() {
			ai.log.Infow("switching to main model")
			ai.isAlt.Store(false)
		})
	}

	return ai.generate(chatID, chat, nTry+1)
}

type AIReply struct {
	Text     string
	AtEnd    bool
	CtxLen   int
	ReplyLen int
}

func (ai *AI) GetReply(chatID int64, userMsg string, forceKeep bool) (AIReply, bool) {
	beginTime := time.Now().UnixNano()

	chat, ok := ai.chats.Get(chatID)
	if !ok {
		ai.log.Warnw("chat is not started", "chat_id", chatID)
		return AIReply{}, false
	}

	chat.hstLock.Lock()
	defer chat.hstLock.Unlock()

	if !forceKeep && chat.isExpired(ai.maxDur) {
		chat.restart()
	}

	chat.addUserMessage(userMsg)

	resp, ok := ai.generate(chatID, chat, 1)
	if !ok {
		chat.removeLastMessage()
		return AIReply{}, false
	}

	if len(resp.Choices) == 0 {
		ai.log.Warnw("no content returned from model", "chat_id", chatID)
		return AIReply{}, false
	}

	if len(resp.Choices) > 1 {
		ai.log.Warnf("model returned %d choices instead of one", len(resp.Choices))
	}

	reply := AIReply{
		Text:   resp.Choices[0].Content,
		AtEnd:  resp.Choices[0].StopReason != "length",
		CtxLen: chat.curCtx,
	}
	if reply.Text == "" {
		ai.log.Warnw("model reply content is empty", "chat_id", chatID)
		return AIReply{}, false
	}

	chat.addBotMessage(reply.Text, ai.maxTok)
	reply.ReplyLen = chat.msgLens[len(chat.msgLens)-1]

	endTime := time.Now().UnixNano()
	duration := float64(endTime-beginTime) / 1000000
	ai.log.Infow("ai message",
		"chat_id", chatID,
		"size", reply.ReplyLen,
		"at_end", reply.AtEnd,
		"dur", fmt.Sprintf("%.2f", duration))

	return reply, true
}

func (ai *AI) GetAllMessages() []DialogMessage {
	chats := ai.chats.PeekAll()
	messages := make([]DialogMessage, 0, len(chats)*3)

	for chatID, chat := range chats {
		for _, message := range chat.messages {
			messages = append(messages, DialogMessage{
				ChatID: chatID,
				Text:   message.Parts[0].(llms.TextContent).Text,
			})
		}
	}

	return messages
}

func (ai *AI) AddAllMessages(messages []DialogMessage, maxHst map[int64]int) {
	var chat *aiChat
	var chatID int64
	for _, msg := range messages {
		if chatID != msg.ChatID {
			chatID = msg.ChatID
			chat = ai.createChat(chatID, msg.Text, maxHst[chatID])
		} else if len(chat.messages)%2 == 1 {
			chat.addUserMessage(msg.Text)
		} else {
			chat.addBotMessage(msg.Text, ai.maxTok)
		}
	}
}
