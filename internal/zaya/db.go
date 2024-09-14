package zaya

import (
	"database/sql"
	"github.com/glebarez/sqlite"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"time"
)

type DB struct {
	db  *gorm.DB
	log *zap.SugaredLogger
	cfg ChatConfig
}

type ChatConfig struct {
	ChatID     int64 `gorm:"primaryKey;autoIncrement:false"`
	Freq       int
	MaxHistory int
	Nickname   string
	Prompt     string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt
}

type BotRole struct {
	gorm.Model
	ChatID     int64
	MaxHistory int `koanf:"max_history"`
	Lang       string
	Name       string
	Example    string
	Nickname   string
	Prompt     string
}

type BotRoleList struct {
	Roles []BotRole
}

type DialogMessage struct {
	gorm.Model
	ChatID int64
	Text   string
}

func LoadDatabase(path string, defaultCfg ChatConfig) (*DB, bool) {
	log := zap.L().Named("db").Sugar()
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	err = db.AutoMigrate(&ChatConfig{}, &BotRole{}, &DialogMessage{})
	if err != nil {
		log.Error(err)
		return nil, false
	}

	return &DB{
		db:  db,
		log: log,
		cfg: defaultCfg,
	}, true
}

func (db *DB) UploadGlobalRoles(path string) {
	var kConf = koanf.New("/")

	var cfg BotRoleList

	err := kConf.Load(file.Provider(path), toml.Parser())
	if err != nil {
		db.log.Warn(err)
		return
	}

	err = kConf.Unmarshal("", &cfg)
	if err != nil {
		db.log.Warn(err)
		return
	}

	for _, role := range cfg.Roles {
		if role.Name == "" {
			db.log.Warnw("name is empty")
		}

		if role.ID != 0 {
			db.log.Warnw("id != 0", "name", role.Name)
			continue
		}

		if role.ChatID != 0 {
			db.log.Warnw("chat_id != 0", "name", role.Name)
			continue
		}

		if role.Lang == "" {
			db.log.Warnw("lang is empty", "name", role.Name)
			continue
		}

		if role.Nickname == "" {
			db.log.Warnw("nickname is empty", "name", role.Name)
			continue
		}

		if role.Prompt == "" {
			db.log.Warnw("prompt is empty", "name", role.Name)
			continue
		}

		db.log.Infow("upload role", "name", role.Name)
		db.saveRole(&role)
	}
}

func (db *DB) LoadChatConfig(chatID int64) *ChatConfig {
	var cfg ChatConfig
	db.db.Limit(1).Find(&cfg, chatID)

	if cfg.ChatID != chatID {
		cfg = db.cfg
		cfg.ChatID = chatID
		db.db.Create(&cfg)
	}

	return &cfg
}

func (db *DB) SetFreq(chatID int64, freq int) {
	tx := db.db.Model(&ChatConfig{}).Where(chatID).
		Updates(&ChatConfig{Freq: freq})

	if tx.RowsAffected < 1 {
		cfg := db.cfg
		cfg.ChatID = chatID
		cfg.Freq = freq
		db.db.Create(&cfg)
	}
}

func (db *DB) SetNickname(chatID int64, nickname string) {
	tx := db.db.Model(&ChatConfig{}).Where(chatID).
		Updates(&ChatConfig{Nickname: nickname})

	if tx.RowsAffected < 1 {
		cfg := db.cfg
		cfg.ChatID = chatID
		cfg.Nickname = nickname
		db.db.Create(&cfg)
	}
}

func (db *DB) SetPrompt(chatID int64, prompt string) {
	tx := db.db.Model(&ChatConfig{}).Where(chatID).
		Updates(&ChatConfig{Prompt: prompt})

	if tx.RowsAffected < 1 {
		cfg := db.cfg
		cfg.ChatID = chatID
		cfg.Prompt = prompt
		db.db.Create(&cfg)
	}
}

func (db *DB) SetMaxHistory(chatID int64, maxHistory int) {
	tx := db.db.Model(&ChatConfig{}).Where(chatID).
		Updates(&ChatConfig{MaxHistory: maxHistory})

	maxHistory = clampMaxHistory(maxHistory)

	if tx.RowsAffected < 1 {
		cfg := db.cfg
		cfg.ChatID = chatID
		cfg.MaxHistory = maxHistory
		db.db.Create(&cfg)
	}
}

func clampMaxHistory(maxHistory int) int {
	if maxHistory == 0 {
		maxHistory = 50
	} else if maxHistory > 50 {
		maxHistory = 50
	}

	return maxHistory
}

func (db *DB) LoadAllRoleNames(chatID int64) []BotRole {
	var roles []BotRole

	db.db.
		Select("id, name").
		Where("chat_id = 0").
		Or("chat_id = ?", chatID).
		Order("lang asc").
		Order("chat_id asc").
		Order("name asc").
		Find(&roles)

	return roles
}

func (db *DB) LoadChatRoleNames(chatID int64) []BotRole {
	var roles []BotRole

	db.db.
		Select("id, name").
		Where("chat_id = ?", chatID).
		Order("lang asc").
		Order("name asc").
		Find(&roles)

	return roles
}

func (db *DB) saveRole(role *BotRole) {
	role.MaxHistory = clampMaxHistory(role.MaxHistory)

	tx := db.db.
		Model(&BotRole{}).
		Where("chat_id = ?", role.ChatID).
		Where("name = ?", role.Name).
		Updates(role)

	if tx.RowsAffected < 1 {
		db.db.Create(role)
	}
}

func (db *DB) SaveRole(chatID int64, lang, name string) {
	cfg := db.LoadChatConfig(chatID)

	role := &BotRole{
		ChatID:     chatID,
		MaxHistory: cfg.MaxHistory,
		Lang:       lang,
		Name:       name,
		Nickname:   cfg.Nickname,
		Prompt:     cfg.Prompt,
	}

	db.saveRole(role)
}

func (db *DB) RemoveRole(chatID int64, roleID uint) bool {
	tx := db.db.
		Where("id = ?", roleID).
		Where("chat_id = ?", chatID).
		Delete(&BotRole{})

	return tx.RowsAffected > 0
}

func (db *DB) SetRole(chatID int64, roleID uint) (*BotRole, bool) {
	role := &BotRole{}

	db.db.First(role, roleID)

	if role.ID == 0 || (role.ChatID != 0 && role.ChatID != chatID) {
		return nil, false
	}

	tx := db.db.Model(&ChatConfig{}).Where(chatID).
		Updates(&ChatConfig{
			MaxHistory: role.MaxHistory,
			Nickname:   role.Nickname,
			Prompt:     role.Prompt,
		})

	if tx.RowsAffected < 1 {
		cfg := db.cfg
		cfg.ChatID = chatID
		cfg.MaxHistory = role.MaxHistory
		cfg.Nickname = role.Nickname
		cfg.Prompt = role.Prompt
		db.db.Create(&cfg)
	}

	return role, true
}

func (db *DB) SaveMessages(messages []DialogMessage) {
	err := db.db.Transaction(func(tx *gorm.DB) error {
		var maxID sql.NullInt64
		err := tx.Model(&DialogMessage{}).
			Select("MAX(id)").
			Pluck("MAX(id)", &maxID).Error
		if err != nil {
			return err
		}

		err = tx.Create(messages).Error
		if err != nil {
			return err
		}

		if maxID.Valid {
			err = tx.Delete(&DialogMessage{}, "id <= ?", maxID.Int64).Error
		}

		return err
	})

	if err != nil {
		db.log.Warnw(err.Error())
	}
}

func (db *DB) LoadMaxHistory() (map[int64]int, bool) {
	configs := make([]*ChatConfig, 0)

	err := db.db.Find(&configs).Error
	if err != nil {
		db.log.Warnw(err.Error())
	}

	maxHst := make(map[int64]int)
	for _, cfg := range configs {
		maxHst[cfg.ChatID] = cfg.MaxHistory
	}

	return maxHst, err == nil
}

func (db *DB) LoadMessages() ([]DialogMessage, bool) {
	messages := make([]DialogMessage, 0)

	err := db.db.Find(&messages).Order("id asc").Error
	if err != nil {
		db.log.Warnw(err.Error())
	}

	return messages, err == nil
}

func (db *DB) GetChatCount() int64 {
	var cnt int64
	db.db.Model(&ChatConfig{}).Count(&cnt)
	return cnt
}

func (db *DB) GetCustomRoleCount() int64 {
	var cnt int64
	db.db.Model(&BotRole{}).Where("chat_id <> 0").Count(&cnt)
	return cnt
}

func (db *DB) LoadAllChatIDs() []int64 {
	var chatIDs []int64

	tx := db.db.Model(&ChatConfig{}).
		Pluck("chat_id", &chatIDs)
	if tx.Error != nil {
		db.log.Warnw(tx.Error.Error())
	}

	return chatIDs
}
