package zaya

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/erni27/imcache"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/mistral"
	"github.com/tmc/langchaingo/llms/openai"
	"go.uber.org/zap"
)

type aiChat struct {
	messages []llms.MessageContent
	msgLens  []int
	msgSizes []int
	curCtx   int
	curSize  int
	maxCtx   int
	maxSize  int
	maxHst   int
	lastTime time.Time
	hstLock  sync.Mutex
	log      *zap.SugaredLogger
}

func newAiChat(prompt string, nCtx, maxSize, maxHistory int, log *zap.SugaredLogger) *aiChat {
	chat := &aiChat{
		messages: make([]llms.MessageContent, 0, 3),
		msgLens:  make([]int, 0, 3),
		msgSizes: make([]int, 0, 3),
		maxCtx:   nCtx,
		maxSize:  maxSize,
		maxHst:   maxHistory,
		lastTime: time.Now(),
		log:      log,
	}

	chat.addMessage(llms.ChatMessageTypeSystem, prompt, "", nil, nil, nil, 4000)

	return chat
}

func getMessageLen(text string, maxTextTok int, doc string, img *Image, audio *Audio, video *Video) int {
	res := min(len(text), maxTextTok)
	res += len(doc)
	if img != nil {
		res += calculateImageTokens(img.Width, img.Height)
		res += len(img.Caption)
	}
	if audio != nil {
		res += audio.Duration * 32
		res += len(audio.Caption)
	}
	if video != nil {
		res += calculateVideoTokens(video.Width, video.Height, video.Duration)
		res += len(video.Caption)
	}
	return res
}

func calculateImageTokens(width, height int) int {
	if width <= 384 && height <= 384 {
		return 258
	}

	// Calculate number of 768x768 tiles needed
	tilesX := (width + 767) / 768  // Ceiling division
	tilesY := (height + 767) / 768 // Ceiling division
	totalTiles := tilesX * tilesY

	return totalTiles * 258
}

func calculateVideoTokens(width, height, duration int) int {
	if width <= 384 && height <= 384 {
		return duration * 98
	}
	return duration * 290
}

func (chat *aiChat) addMessage(role llms.ChatMessageType, text, doc string, img *Image, audio *Audio, video *Video, maxTok int) {
	msg := llms.MessageContent{
		Role:  role,
		Parts: make([]llms.ContentPart, 0),
	}

	size := 0
	if img != nil {
		imagePart := llms.BinaryPart("image/jpeg", img.Data)
		msg.Parts = append(msg.Parts, imagePart)
		size += len(imagePart.Data)

		if img.Caption != "" {
			captionPart := llms.TextPart(img.Caption)
			msg.Parts = append(msg.Parts, captionPart)
			size += len(captionPart.Text)
		}
	}

	if audio != nil {
		audioPart := llms.BinaryPart("audio/mpeg", audio.Data)
		msg.Parts = append(msg.Parts, audioPart)
		size += len(audioPart.Data)

		if audio.Caption != "" {
			captionPart := llms.TextPart(audio.Caption)
			msg.Parts = append(msg.Parts, captionPart)
			size += len(captionPart.Text)
		}
	}

	if video != nil {
		videoPart := llms.BinaryPart("video/mp4", video.Data)
		msg.Parts = append(msg.Parts, videoPart)
		size += len(videoPart.Data)

		if video.Caption != "" {
			captionPart := llms.TextPart(video.Caption)
			msg.Parts = append(msg.Parts, captionPart)
			size += len(captionPart.Text)
		}
	}

	if doc != "" {
		docPart := llms.TextPart(doc)
		msg.Parts = append(msg.Parts, docPart)
		size += len(docPart.Text)
	}

	if text != "" {
		textPart := llms.TextPart(text)
		msg.Parts = append(msg.Parts, textPart)
		size += len(textPart.Text)
	}

	chat.messages = append(chat.messages, msg)
	chat.lastTime = time.Now()

	msgLen := getMessageLen(text, maxTok, doc, img, audio, video)
	chat.msgLens = append(chat.msgLens, msgLen)
	chat.curCtx += msgLen

	chat.msgSizes = append(chat.msgSizes, size)
	chat.curSize += size

	if (chat.maxCtx > 0 && chat.curCtx >= chat.maxCtx) ||
		(chat.maxHst > 0 && len(chat.messages)-1 > chat.maxHst) {
		chat.cleanHistory()
	}
}

const maxUserTokens = 4000

func (chat *aiChat) addUserMessage(text string, doc string, img *Image, audio *Audio, video *Video) {
	chat.addMessage(llms.ChatMessageTypeHuman, text, doc, img, audio, video, maxUserTokens)
}

func (chat *aiChat) addUserTextMessage(text string) {
	chat.addMessage(llms.ChatMessageTypeHuman, text, "", nil, nil, nil, maxUserTokens)
}

func (chat *aiChat) addUserImageMessage(img *Image) {
	chat.addMessage(llms.ChatMessageTypeHuman, "", "", img, nil, nil, maxUserTokens)
}

func (chat *aiChat) addUserAudioMessage(audio *Audio) {
	chat.addMessage(llms.ChatMessageTypeHuman, "", "", nil, audio, nil, maxUserTokens)
}

func (chat *aiChat) addUserVideoMessage(video *Video) {
	chat.addMessage(llms.ChatMessageTypeHuman, "", "", nil, nil, video, maxUserTokens)
}

func (chat *aiChat) addBotMessage(text string, maxTok int) {
	chat.addMessage(llms.ChatMessageTypeAI, text, "", nil, nil, nil, maxTok)
}

func (chat *aiChat) removeLastMessage() {
	chat.curCtx -= chat.msgLens[len(chat.msgLens)-1]
	chat.msgLens = chat.msgLens[:len(chat.msgLens)-1]
	chat.curSize -= chat.msgSizes[len(chat.msgSizes)-1]
	chat.msgSizes = chat.msgSizes[:len(chat.msgSizes)-1]
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
		chat.curSize -= chat.msgSizes[rmCnt]
	}
	for (rmCnt == 0 || rmCnt%2 != 0) && rmCnt < msgCnt {
		rmCnt++
		chat.curCtx -= chat.msgLens[rmCnt]
		chat.curSize -= chat.msgSizes[rmCnt]
	}

	chat.msgLens = append(chat.msgLens[:1], chat.msgLens[rmCnt+1:]...)
	chat.msgSizes = append(chat.msgSizes[:1], chat.msgSizes[rmCnt+1:]...)
	chat.messages = append(chat.messages[:1], chat.messages[rmCnt+1:]...)

	chat.log.Infow("clean history",
		"removed", rmCnt,
		"left", len(chat.messages),
		"ctx", chat.curCtx)
}

func (chat *aiChat) cleanData() {
	rmCnt := 0
	for i := range chat.messages {
		if chat.curSize < chat.maxSize {
			break
		}

		msg := chat.messages[i]
		if len(msg.Parts) == 1 {
			_, ok := msg.Parts[0].(llms.TextContent)
			if ok {
				continue
			}
		}

		text := ""
		for i := len(msg.Parts) - 1; i >= 0; i-- {
			if textPart, ok := msg.Parts[i].(llms.TextContent); ok {
				text = textPart.Text
				break
			}
		}
		if text == "" {
			text = "(uploaded file)"
		}

		rmCnt += len(msg.Parts) - 1
		rmSize := chat.msgSizes[i] - len(text)
		chat.msgSizes[i] = len(text)
		chat.curSize -= rmSize

		msg.Parts = make([]llms.ContentPart, 1)
		msg.Parts[0] = llms.TextPart(text)

		chat.messages[i] = msg
	}

	chat.log.Infow("clean data",
		"removed files", rmCnt,
		"left size", chat.curSize,
		"ctx", chat.curCtx)
}

func (chat *aiChat) isExpired(maxDur time.Duration) bool {
	return time.Since(chat.lastTime) > maxDur
}

func (chat *aiChat) restart() {
	chat.curCtx = chat.msgLens[0]
	chat.curSize = chat.msgSizes[0]
	chat.msgLens = chat.msgLens[:1]
	chat.msgSizes = chat.msgSizes[:1]
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
	maxSize int
	accImgs bool
	accAud  bool
	accVid  bool
	maxDur  time.Duration
	chatExp imcache.Expiration
}

func NewAI(cfg AiConfig) (*AI, bool) {
	ai := &AI{
		opts:    make([]llms.CallOption, 0),
		log:     zap.L().Named("ai").Sugar(),
		maxCtx:  cfg.NCtx - cfg.MaxTok,
		maxTok:  cfg.MaxTok,
		maxSize: cfg.MaxSize,
		accImgs: cfg.Accept.Images,
		accAud:  cfg.Accept.Audio,
		accVid:  cfg.Accept.Video,
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
	case "googleai":
		opts := make([]googleai.Option, 0)
		opts = append(opts, googleai.WithHarmThreshold(googleai.HarmBlockNone))
		if cfg.ApiKey != "" {
			opts = append(opts, googleai.WithAPIKey(cfg.ApiKey))
		}
		if cfg.Model != "" {
			opts = append(opts, googleai.WithDefaultModel(cfg.Model))
		}
		ai.llm, err = googleai.New(context.Background(), opts...)

		opts = opts[:2]
		if cfg.AltModel != "" {
			opts = append(opts, googleai.WithDefaultModel(cfg.AltModel))
		}
		ai.altLlm, err = googleai.New(context.Background(), opts...)
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
	chat := newAiChat(prompt, ai.maxCtx, ai.maxSize, maxHistory, ai.log)
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
	if chat.curSize > chat.maxSize {
		chat.cleanData()
	}

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

type Image struct {
	Data    []byte
	Caption string
	Width   int
	Height  int
}

type Audio struct {
	Data     []byte
	Caption  string
	Duration int
}

type Video struct {
	Data     []byte
	Caption  string
	Duration int
	Width    int
	Height   int
}

type AIRequest struct {
	ChatID    int64
	Text      string
	Document  string
	Image     *Image
	Audio     *Audio
	Video     *Video
	ForceKeep bool
}

func (req AIRequest) isEmpty() bool {
	return req.Text == "" && req.Document == "" &&
		(req.Image == nil || len(req.Image.Data) == 0) &&
		(req.Audio == nil || len(req.Audio.Data) == 0) &&
		(req.Video == nil || len(req.Video.Data) == 0)
}

type AIReply struct {
	Text     string
	AtEnd    bool
	CtxLen   int
	ReplyLen int
}

func (ai *AI) GetReply(req AIRequest) (AIReply, bool) {
	beginTime := time.Now().UnixNano()

	if !ai.accImgs {
		req.Image = nil
	}
	if !ai.accAud {
		req.Audio = nil
	}
	if !ai.accVid {
		req.Video = nil
	}
	if req.isEmpty() {
		return AIReply{}, false
	}

	chat, ok := ai.chats.Get(req.ChatID)
	if !ok {
		ai.log.Warnw("chat is not started", "chat_id", req.ChatID)
		return AIReply{}, false
	}

	chat.hstLock.Lock()
	defer chat.hstLock.Unlock()

	if !req.ForceKeep && chat.isExpired(ai.maxDur) {
		chat.restart()
	}

	chat.addUserMessage(req.Text, req.Document, req.Image, req.Audio, req.Video)

	resp, ok := ai.generate(req.ChatID, chat, 1)
	if !ok {
		chat.removeLastMessage()
		return AIReply{}, false
	}

	if len(resp.Choices) == 0 {
		ai.log.Warnw("no content returned from model", "chat_id", req.ChatID)
		return AIReply{}, false
	}

	if len(resp.Choices) > 1 {
		ai.log.Warnf("model returned %d choices instead of one", len(resp.Choices))
	}

	choice := resp.Choices[0]
	reply := AIReply{
		Text:   choice.Content,
		AtEnd:  choice.StopReason != "length" && choice.StopReason != "FinishReasonMaxTokens",
		CtxLen: chat.curCtx,
	}
	if reply.Text == "" {
		ai.log.Warnw("model reply content is empty", "chat_id", req.ChatID)
		return AIReply{}, false
	}

	chat.addBotMessage(reply.Text, ai.maxTok)
	reply.ReplyLen = chat.msgLens[len(chat.msgLens)-1]

	if chat.curSize > chat.maxSize {
		chat.cleanData()
	}

	endTime := time.Now().UnixNano()
	duration := float64(endTime-beginTime) / 1000000
	ai.log.Infow("ai message",
		"chat_id", req.ChatID,
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
			msg := DialogMessage{
				ChatID: chatID,
			}
			text, ok := message.Parts[len(message.Parts)-1].(llms.TextContent)
			if ok {
				msg.Text = text.Text
			} else {
				msg.Text = "(uploaded file)"
			}
			messages = append(messages, msg)
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
			chat.addUserMessage(msg.Text, "", nil, nil, nil)
		} else {
			chat.addBotMessage(msg.Text, ai.maxTok)
		}
	}
}
