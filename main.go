package main

import (
	"go.uber.org/zap"
	"neuralzaya/internal/zaya"
	"os"
	"os/signal"
)

func main() {
	cfg, err := zaya.LoadConfig()
	if err != nil {
		panic(err)
	}

	var zapLogger *zap.Logger
	if cfg.Release {
		zapLogger, err = zap.NewProduction(zap.WithCaller(false))
	} else {
		zapLogger, err = zap.NewDevelopment(zap.WithCaller(false))
	}
	if err != nil {
		panic(err)
	}
	defer func() { _ = zapLogger.Sync() }()

	zap.ReplaceGlobals(zapLogger)
	zap.RedirectStdLog(zapLogger)
	logger := zapLogger.Sugar()

	ai, ok := zaya.NewAI(cfg.Ai)
	if !ok {
		logger.Panic("can't create AI")
	}

	defaultChatConfig := zaya.ChatConfig{
		Freq:     cfg.DefaultCfg.Freq,
		Nickname: cfg.DefaultCfg.Nickname,
		Prompt:   cfg.DefaultCfg.Prompt,
	}
	db, ok := zaya.LoadDatabase(cfg.DBPath, defaultChatConfig)
	if !ok {
		logger.Panic("can't load database")
	}
	if cfg.RolesPath != "" {
		db.UploadGlobalRoles(cfg.RolesPath)
	}

	{
		allMessages, ok := db.LoadMessages()
		if !ok {
			logger.Panic("can't load messages")
		}
		maxHst, ok := db.LoadMaxHistory()
		if !ok {
			logger.Panic("can't load history limits")
		}
		ai.AddAllMessages(allMessages, maxHst)
	}

	defer func() {
		allMessages := ai.GetAllMessages()
		db.SaveMessages(allMessages)
	}()

	bot, ok := zaya.NewBot(cfg, ai, db)
	if !ok {
		logger.Panic("can't create bot")
	}

	bot.Start()
	defer bot.Stop()

	quit := make(chan os.Signal)
	signal.Notify(quit, os.Interrupt)
	<-quit
}
